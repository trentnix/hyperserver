package blog

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxDownloadBytes   = 32 * 1024 * 1024
	maxDownloadStorage = 1024 * 1024 * 1024
	maxPostDownloads   = 20
)

// downloadStore stores immutable attachments separately from generated images.
// The caller owns every returned file and must close it.
type downloadStore interface {
	Put(context.Context, []byte, string) (string, error)
	Open(context.Context, string) (*os.File, error)
}

type fileDownloadStore struct{ root *os.Root }

func openDownloadStore(uploads *os.Root) (*fileDownloadStore, error) {
	if err := uploads.MkdirAll("files", 0700); err != nil {
		return nil, err
	}
	root, err := uploads.OpenRoot("files")
	if err != nil {
		return nil, err
	}
	return &fileDownloadStore{root}, nil
}

func downloadType(id string) string {
	switch filepath.Ext(id) {
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return ""
}

func validDownloadID(id string) bool {
	digest, extension, ok := strings.Cut(id, ".")
	if !ok || len(digest) != 64 || (extension != "pdf" && extension != "zip" && extension != "txt") {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil && digest == strings.ToLower(digest) && !strings.ContainsAny(id, `/\`)
}

// validateDownload checks file signatures and structure, never the submitted MIME
// type. ZIP entries are not extracted. All accepted formats are served as attachments.
func validateDownload(data []byte, filename string) (string, error) {
	if len(data) > maxDownloadBytes {
		return "", contentError("Choose a file of at most 32 MiB.")
	}
	extension := strings.ToLower(filepath.Ext(filename))
	valid := false
	switch extension {
	case ".pdf":
		valid = len(data) >= 9 && bytes.HasPrefix(data, []byte("%PDF-")) &&
			(data[5] == '1' || data[5] == '2') && data[6] == '.' && data[7] >= '0' && data[7] <= '9' &&
			bytes.Contains(data[max(0, len(data)-1024):], []byte("%%EOF"))
	case ".zip":
		_, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		valid = err == nil
	case ".txt":
		valid = utf8.Valid(data) && (len(data) == 0 || strings.HasPrefix(http.DetectContentType(data), "text/plain"))
		for _, r := range string(data) {
			if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
				valid = false
				break
			}
		}
	}
	if !valid {
		return "", contentError("Choose a valid PDF, ZIP, or UTF-8 text file with a .pdf, .zip, or .txt extension.")
	}
	return extension, nil
}

func downloadID(data []byte, extension string) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]) + extension
}

func (s *fileDownloadStore) Put(ctx context.Context, data []byte, extension string) (string, error) {
	id := downloadID(data, extension)
	if !validDownloadID(id) {
		return "", errors.New("invalid download type")
	}
	temporary := ".upload-" + rand.Text()
	file, err := s.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer s.root.Remove(temporary)
	defer file.Close()
	if _, err := io.Copy(contextWriter{ctx, file}, bytes.NewReader(data)); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Linking publishes without replacing existing bytes or changing their date.
	if err := s.root.Link(temporary, id); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	stored, err := s.Open(ctx, id)
	if err != nil {
		return "", err
	}
	return id, stored.Close()
}

func (s *fileDownloadStore) Open(ctx context.Context, id string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validDownloadID(id) {
		return nil, errNotFound
	}
	info, err := s.root.Lstat(id)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.root.Open(id)
}

// downloadMedia shares the existing cleanup boundary without exposing attachments
// as image-library choices. Both directories remain under the backup uploads root.
type downloadMedia struct {
	images mediaStore
	files  *fileDownloadStore
}

func (s downloadMedia) List(ctx context.Context) ([]mediaFile, error) {
	images, err := s.images.List(ctx)
	if err != nil {
		return nil, err
	}
	files, err := s.files.List(ctx)
	return append(images, files...), err
}

func (s downloadMedia) Remove(ctx context.Context, id string, version int64) error {
	if validImageID(id) {
		return s.images.Remove(ctx, id, version)
	}
	if !validDownloadID(id) {
		return errNotFound
	}
	file, err := s.files.Open(ctx, id)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	file.Close()
	if err != nil {
		return err
	}
	if info.ModTime().UnixNano() != version {
		return errMediaChanged
	}
	return s.files.root.Remove(id)
}

func (s *fileDownloadStore) List(ctx context.Context) ([]mediaFile, error) {
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return nil, err
	}
	var files []mediaFile
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !validDownloadID(entry.Name()) || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		files = append(files, mediaFile{ID: entry.Name(), Size: info.Size(), Modified: info.ModTime()})
	}
	return files, nil
}

func (f mediaFile) IsDownload() bool { return validDownloadID(f.ID) }
