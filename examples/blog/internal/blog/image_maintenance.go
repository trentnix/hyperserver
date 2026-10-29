package blog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// OptimizeImages regenerates display copies from preserved originals, including
// legacy uploads. Stop the blog first. Each completed image is published atomically.
// On failure, the count reports completed images. The failed image stays unchanged.
func OptimizeImages(ctx context.Context, directory, workingDirectory string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	store, err := openImageStore(directory, workingDirectory)
	if err != nil {
		return 0, err
	}
	defer store.Close()
	files, err := store.List(ctx)
	if err != nil {
		return 0, err
	}
	for i, file := range files {
		if err := optimizeStoredImage(ctx, store, file.ID); err != nil {
			return i, fmt.Errorf("optimize %s: %w", file.ID, err)
		}
	}
	return len(files), nil
}

// optimizeStoredImage verifies original identity before replacing any stored bytes.
func optimizeStoredImage(ctx context.Context, store *fileImageStore, id string) error {
	original, err := store.OpenVariant(ctx, id, originalImage)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, original.Content}, maxImageBytes+1))
	closeErr := original.Content.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	if !strings.HasPrefix(id, hex.EncodeToString(digest[:])+".") {
		return errors.New("original no longer matches its image ID")
	}
	upload, err := prepareImage(ctx, data)
	if err != nil {
		return err
	}
	if upload.MediaType != original.MediaType {
		return errors.New("original no longer matches its image type")
	}
	_, err = store.put(ctx, upload, original.Modified)
	return err
}
