package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestExistingArchivePreserved(t *testing.T) {
	cfg := fixture(t)
	directory := t.TempDir()
	archive := filepath.Join(directory, "backup.zip")
	original := []byte("existing backup must survive")
	if err := os.WriteFile(archive, original, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(directory, "alias.zip")
	if err := os.Symlink(archive, alias); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{archive, alias} {
		if err := Create(t.Context(), cfg, target); err == nil {
			t.Fatal("backup accepted existing output")
		}
		if got, err := os.ReadFile(archive); err != nil || !bytes.Equal(got, original) {
			t.Fatalf("existing backup changed: %q, %v", got, err)
		}
	}
}

func TestConcurrentBackup(t *testing.T) {
	cfg := fixture(t)
	parent := t.TempDir()
	archive := filepath.Join(parent, "backup.zip")
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- Create(t.Context(), cfg, archive)
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful backups, want exactly one", successes)
	}
	if err := Verify(t.Context(), archive); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 || entries[0].Name() != "backup.zip" {
		t.Fatal("unexpected backup artifacts", entries, err)
	}
}

func TestTruncatedArchive(t *testing.T) {
	cfg := fixture(t)
	archive := filepath.Join(t.TempDir(), "backup.zip")
	if err := Create(t.Context(), cfg, archive); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(archive, info.Size()/2); err != nil {
		t.Fatal(err)
	}
	if err := Verify(t.Context(), archive); err == nil {
		t.Fatal("truncated archive verified")
	}
}

// cancelingReader simulates cancellation after bytes have already been copied.
type cancelingReader struct {
	cancel context.CancelFunc
}

func (r cancelingReader) Read(p []byte) (int, error) {
	n := copy(p, "partial data")
	r.cancel()
	return n, nil
}

func TestCopyStopsDuringCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	n, err := io.Copy(&output, contextReader{ctx, cancelingReader{cancel}})
	if !errors.Is(err, context.Canceled) || n != int64(len("partial data")) || output.String() != "partial data" {
		t.Fatalf("copy did not stop after cancellation: n=%d, output=%q, err=%v", n, output.String(), err)
	}
}
