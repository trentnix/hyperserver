package blog

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// imageStore stores validated uploads under immutable IDs, independently of posts.
type imageStore interface {
	Put(context.Context, string, []byte) (string, error)
	// Open returns errNotFound for an unknown ID. The caller closes Content.
	Open(context.Context, string) (storedImage, error)
}

// storedImage pairs a seekable body with the metadata needed for HTTP serving.
// Digest is the lowercase hexadecimal SHA-256 digest of Content.
type storedImage struct {
	Content   io.ReadSeekCloser
	MediaType string
	Modified  time.Time
	Digest    string
}

// fileImageStore confines file access to one application-owned uploads directory.
type fileImageStore struct{ root *os.Root }

// openImageStore resolves relative paths against the application configuration directory.
// The caller owns the returned store and must close it after requests have drained.
func openImageStore(directory, workingDirectory string) (*fileImageStore, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, errors.New("blog.uploadsDirectory is required")
	}
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(workingDirectory, directory)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	return &fileImageStore{root: root}, nil
}

// Close releases the directory handle, not the uploaded files.
func (s *fileImageStore) Close() error { return s.root.Close() }

// Put writes validated bytes to a temporary file before publishing the complete image.
// Identical uploads share an ID. Concurrent replacements contain identical bytes.
func (s *fileImageStore) Put(ctx context.Context, media string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if media != "image/png" && media != "image/jpeg" && media != "image/gif" {
		return "", errors.New("unsupported image type")
	}
	digest := sha256.Sum256(data)
	id := hex.EncodeToString(digest[:]) + "." + strings.TrimPrefix(media, "image/")
	temporary := ".upload-" + rand.Text()
	file, err := s.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer s.root.Remove(temporary)
	defer file.Close()
	if _, err := file.Write(data); err != nil {
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
	if err := s.root.Rename(temporary, id); err != nil {
		return "", err
	}
	return id, nil
}

// Open accepts only generated image IDs and never exposes directory listings.
// File access stays inside the uploads directory, including when symlinks exist.
func (s *fileImageStore) Open(ctx context.Context, id string) (storedImage, error) {
	if err := ctx.Err(); err != nil {
		return storedImage{}, err
	}
	if !validImageID(id) {
		return storedImage{}, errNotFound
	}
	digest, extension, _ := strings.Cut(id, ".")
	file, err := s.root.Open(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			err = errNotFound
		}
		return storedImage{}, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err == nil {
			err = errNotFound
		}
		return storedImage{}, err
	}
	return storedImage{Content: file, MediaType: "image/" + extension, Modified: info.ModTime(), Digest: digest}, nil
}

// mediaFile describes an upload without opening or decoding its contents.
type mediaFile struct {
	ID       string
	Size     int64
	Modified time.Time
}

// Version detects uploads replaced after a cleanup page was loaded.
func (f mediaFile) Version() int64 { return f.Modified.UnixNano() }

// mediaStore browses and removes generated upload files, never application assets.
type mediaStore interface {
	List(context.Context) ([]mediaFile, error)
	Remove(context.Context, string, int64) error
}

var errMediaChanged = errors.New("the upload changed since it was listed")

// List excludes temporary files, directories, symlinks, and unrelated files.
func (s *fileImageStore) List(ctx context.Context) ([]mediaFile, error) {
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return nil, err
	}
	var files []mediaFile
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !validImageID(entry.Name()) || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			files = append(files, mediaFile{ID: entry.Name(), Size: info.Size(), Modified: info.ModTime()})
		}
	}
	return files, ctx.Err()
}

// Remove checks the selected version before unlinking one regular upload file.
// The module serializes this operation against uploads and content saves.
func (s *fileImageStore) Remove(ctx context.Context, id string, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validImageID(id) {
		return errNotFound
	}
	info, err := s.root.Lstat(id)
	if errors.Is(err, fs.ErrNotExist) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errNotFound
	}
	if info.ModTime().UnixNano() != version {
		return errMediaChanged
	}
	return s.root.Remove(id)
}

// validImageID accepts only names generated by Put, without path components.
func validImageID(id string) bool {
	digest, extension, ok := strings.Cut(id, ".")
	if !ok || len(digest) != sha256.Size*2 || (extension != "png" && extension != "jpeg" && extension != "gif") {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil && digest == strings.ToLower(digest)
}
