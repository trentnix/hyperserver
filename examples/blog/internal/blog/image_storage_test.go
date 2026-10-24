package blog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestImageStore(t *testing.T) *fileImageStore {
	t.Helper()
	store, err := openImageStore("uploads", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestFileImagePersistence checks relative paths, immutable IDs, and reopening storage.
func TestFileImagePersistence(t *testing.T) {
	directory := t.TempDir()
	store, err := openImageStore("uploads", directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	data := []byte("validated image bytes")
	ids := make(map[string]string)
	for _, media := range []string{"image/png", "image/jpeg", "image/gif"} {
		id, err := store.Put(t.Context(), media, data)
		if err != nil {
			t.Fatal(err)
		}
		ids[media] = id
		if again, err := store.Put(t.Context(), media, data); err != nil || again != id {
			t.Fatalf("duplicate upload: %q, %v", again, err)
		}
		saved, err := os.ReadFile(filepath.Join(directory, "uploads", id))
		if err != nil || !bytes.Equal(saved, data) {
			t.Fatalf("file not saved under configured directory: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openImageStore(filepath.Join(directory, "uploads"), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for media, id := range ids {
		stored, err := reopened.Open(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(stored.Content)
		stored.Content.Close()
		digest := sha256.Sum256(body)
		if err != nil || !bytes.Equal(body, data) || stored.MediaType != media || stored.Modified.IsZero() || stored.Digest != hex.EncodeToString(digest[:]) {
			t.Fatalf("reopened image: %+v, %v", stored, err)
		}
	}
	changed, err := reopened.Put(t.Context(), "image/png", []byte("different content"))
	if err != nil || changed == ids["image/png"] {
		t.Fatalf("changed bytes reused URL: %q, %v", changed, err)
	}
}

// TestFileImageConcurrentAccess checks publication while identical uploads and reads overlap.
func TestFileImageConcurrentAccess(t *testing.T) {
	store := newTestImageStore(t)
	data := bytes.Repeat([]byte("image"), 10000)
	id, err := store.Put(t.Context(), "image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if got, err := store.Put(t.Context(), "image/png", data); err != nil || got != id {
				t.Errorf("concurrent upload: %q, %v", got, err)
			}
		})
		wg.Go(func() {
			stored, err := store.Open(t.Context(), id)
			if err != nil {
				t.Error(err)
				return
			}
			defer stored.Content.Close()
			body, err := io.ReadAll(stored.Content)
			if err != nil || !bytes.Equal(body, data) {
				t.Errorf("reader saw incomplete upload: %v", err)
			}
		})
	}
	wg.Wait()
	entries, err := os.ReadDir(store.root.Name())
	if err != nil || len(entries) != 1 || entries[0].Name() != id {
		t.Fatalf("unexpected files after concurrent uploads: %v, %v", entries, err)
	}
}

// TestFileImagePaths rejects traversal, temporary files, directories, and escaping symlinks.
func TestFileImagePaths(t *testing.T) {
	store := newTestImageStore(t)
	for _, id := range []string{"", ".", "..", "../outside.png", "/etc/passwd", `.\outside.png`, ".upload-secret", strings.Repeat("z", 64) + ".png", strings.Repeat("a", 64) + ".svg", strings.Repeat("a", 64) + ".png/extra"} {
		if _, err := store.Open(t.Context(), id); !errors.Is(err, errNotFound) {
			t.Errorf("ID %q: %v", id, err)
		}
	}
	id := strings.Repeat("a", 64) + ".png"
	if _, err := store.Open(t.Context(), id); !errors.Is(err, errNotFound) {
		t.Fatalf("missing image: %v", err)
	}
	if err := store.root.Mkdir(id, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(t.Context(), id); !errors.Is(err, errNotFound) {
		t.Fatalf("directory image: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "private.png")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	id = strings.Repeat("b", 64) + ".png"
	if err := os.Symlink(outside, filepath.Join(store.root.Name(), id)); err != nil {
		t.Fatal(err)
	}
	if stored, err := store.Open(t.Context(), id); err == nil {
		stored.Content.Close()
		t.Fatal("symlink escaped uploads directory")
	}
}

// TestFileImageFailures checks initialization, cancellation, and cleanup after publication fails.
func TestFileImageFailures(t *testing.T) {
	directory := t.TempDir()
	for _, empty := range []string{"", " "} {
		if _, err := openImageStore(empty, directory); err == nil {
			t.Fatal("missing directory accepted")
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "not-a-directory"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openImageStore("not-a-directory", directory); err == nil {
		t.Fatal("file accepted as upload directory")
	}
	store := newTestImageStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Put(ctx, "image/png", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write: %v", err)
	}
	if _, err := store.Open(ctx, strings.Repeat("a", 64)+".png"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
	if _, err := store.Put(t.Context(), "text/html", nil); err == nil {
		t.Fatal("unsupported media type accepted")
	}
	data := []byte("validated bytes")
	digest := sha256.Sum256(data)
	id := hex.EncodeToString(digest[:]) + ".png"
	if err := store.root.Mkdir(id, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(t.Context(), "image/png", data); err == nil {
		t.Fatal("publication failure ignored")
	}
	entries, err := os.ReadDir(store.root.Name())
	if err != nil || len(entries) != 1 || entries[0].Name() != id {
		t.Fatalf("failed write left temporary files: %v, %v", entries, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(t.Context(), "image/png", data); err == nil {
		t.Fatal("write to closed storage succeeded")
	}
	if _, err := store.Open(t.Context(), id); err == nil {
		t.Fatal("read from closed storage succeeded")
	}
}

// TestModuleImageStorageLifecycle checks ownership and cleanup without changing application setup.
func TestModuleImageStorageLifecycle(t *testing.T) {
	h := newHarness(t)
	images := h.blog.images.(*fileImageStore)
	id, err := images.Put(t.Context(), "image/png", []byte("validated bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.ShutdownContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := images.Open(t.Context(), id); err == nil {
		t.Fatal("shutdown left image storage open")
	}
	if _, err := h.blog.posts.List(t.Context()); err == nil {
		t.Fatal("shutdown left post storage open")
	}
}

// TestModuleImageStorageInitFailure rejects invalid upload configuration before serving routes.
func TestModuleImageStorageInitFailure(t *testing.T) {
	h := newHarness(t)
	settings := h.blog.settings
	settings.Storage.Connection = "failed-init.db"
	if err := os.WriteFile(filepath.Join(h.app.Config.App.WorkingDirectory, "occupied"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"", "occupied"} {
		settings.UploadsDirectory = directory
		m := &module{settings: settings, access: &Access{}}
		err := m.Init(t.Context(), h.app.ApplicationServer)
		if err == nil || !strings.Contains(err.Error(), "open blog uploads") {
			t.Fatalf("invalid directory %q: %v", directory, err)
		}
		if m.posts != nil || m.images != nil || m.close != nil || m.access.emailKey != "" {
			t.Fatal("failed initialization published partial module state")
		}
	}
}
