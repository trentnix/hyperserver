package blog

import (
	"archive/zip"
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
	Put(context.Context, imageUpload) (string, error)
	// Open returns errNotFound for an unknown ID. The caller closes Content.
	Open(context.Context, string) (storedImage, error)
	OpenVariant(context.Context, string, imageVariant) (storedImage, error)
}

// imageVariant selects a fixed representation, never a filesystem path.
type imageVariant string

const (
	displayImage   imageVariant = "display"
	thumbnailImage imageVariant = "thumbnail"
	originalImage  imageVariant = "original"
)

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

// Put atomically publishes one upload, including its original and generated copies.
// ZIP's uncompressed entries keep the files together while allowing seekable reads.
// The original's hash preserves identity across re-uploads and optimization changes.
func (s *fileImageStore) Put(ctx context.Context, upload imageUpload) (string, error) {
	return s.put(ctx, upload, time.Time{})
}

// put lets offline regeneration preserve upload dates while ordinary uploads get a new version.
func (s *fileImageStore) put(ctx context.Context, upload imageUpload, modified time.Time) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	media := upload.MediaType
	if media != "image/png" && media != "image/jpeg" && media != "image/gif" {
		return "", errors.New("unsupported image type")
	}
	if len(upload.Original) == 0 || len(upload.Display) == 0 || len(upload.Thumbnail) == 0 {
		return "", errors.New("incomplete image upload")
	}
	digest := sha256.Sum256(upload.Original)
	id := hex.EncodeToString(digest[:]) + "." + strings.TrimPrefix(media, "image/")
	temporary := ".upload-" + rand.Text()
	file, err := s.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer s.root.Remove(temporary)
	defer file.Close()
	archive := zip.NewWriter(contextWriter{ctx, file})
	for _, part := range []struct {
		name string
		data []byte
	}{
		{string(originalImage), upload.Original},
		{string(displayImage), upload.Display},
		{string(thumbnailImage), upload.Thumbnail},
	} {
		digest := sha256.Sum256(part.data)
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: part.name, Method: zip.Store, Comment: hex.EncodeToString(digest[:])})
		if err != nil {
			return "", err
		}
		if _, err := entry.Write(part.data); err != nil {
			return "", err
		}
	}
	if err := archive.Close(); err != nil {
		return "", err
	}
	if !modified.IsZero() {
		if err := s.root.Chtimes(temporary, modified, modified); err != nil {
			return "", err
		}
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
	return s.OpenVariant(ctx, id, displayImage)
}

// errImageNotOptimized prevents legacy originals from becoming public display copies.
var errImageNotOptimized = errors.New("image requires optimization")

// OpenVariant exposes only the three known entries, never arbitrary archive paths.
// Legacy uploads are available as originals for explicit offline regeneration only.
func (s *fileImageStore) OpenVariant(ctx context.Context, id string, variant imageVariant) (storedImage, error) {
	if err := ctx.Err(); err != nil {
		return storedImage{}, err
	}
	if !validImageID(id) || (variant != displayImage && variant != thumbnailImage && variant != originalImage) {
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
	archive, err := zip.NewReader(file, info.Size())
	if errors.Is(err, zip.ErrFormat) {
		if variant == originalImage {
			return storedImage{Content: file, MediaType: "image/" + extension, Modified: info.ModTime(), Digest: digest}, nil
		}
		file.Close()
		return storedImage{}, errImageNotOptimized
	}
	if err != nil {
		file.Close()
		return storedImage{}, err
	}
	if len(archive.File) != 3 {
		file.Close()
		return storedImage{}, errors.New("incomplete image archive")
	}
	var selected *zip.File
	seen := make(map[string]bool)
	for _, entry := range archive.File {
		_, digestError := hex.DecodeString(entry.Comment)
		known := entry.Name == string(displayImage) || entry.Name == string(thumbnailImage) || entry.Name == string(originalImage)
		validDigest := len(entry.Comment) == 64 && digestError == nil
		if seen[entry.Name] || !known || !validDigest || entry.Method != zip.Store {
			file.Close()
			return storedImage{}, errors.New("invalid image archive")
		}
		seen[entry.Name] = true
		if entry.Name == string(variant) {
			selected = entry
		}
	}
	offset, err := selected.DataOffset()
	if err != nil {
		file.Close()
		return storedImage{}, err
	}
	size := selected.UncompressedSize64
	validSize := size > 0 && size <= 128*1024*1024 && selected.CompressedSize64 == size
	if !validSize || offset < 0 || offset > info.Size() || size > uint64(info.Size()-offset) {
		file.Close()
		return storedImage{}, errors.New("invalid image entry size")
	}
	return storedImage{
		Content:   imageSection{io.NewSectionReader(file, offset, int64(size)), file},
		MediaType: "image/" + extension, Modified: info.ModTime(), Digest: selected.Comment,
	}, nil
}

// imageSection holds the archive open while ServeContent reads or seeks one entry.
type imageSection struct {
	*io.SectionReader
	io.Closer
}

// contextWriter lets storage writes stop between chunks.
type contextWriter struct {
	ctx context.Context
	io.Writer
}

// Write stops a staged upload when the request is canceled.
func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.Writer.Write(p)
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
