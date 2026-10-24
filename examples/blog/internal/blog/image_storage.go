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
	digest, extension, ok := strings.Cut(id, ".")
	if !ok || len(digest) != sha256.Size*2 || (extension != "png" && extension != "jpeg" && extension != "gif") {
		return storedImage{}, errNotFound
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return storedImage{}, errNotFound
	}
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
