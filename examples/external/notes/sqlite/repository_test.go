package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"example.com/greeting/notes"
)

func TestSQLiteFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.db")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Open(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	r, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	note := notes.Note{ID: "00000000-0000-4000-8000-000000000001", Text: "valid", CreatedAt: time.Now().UTC()}
	if _, err := r.db.ExecContext(t.Context(), `CREATE TRIGGER reject_note BEFORE INSERT ON example_notes BEGIN SELECT RAISE(ABORT, 'write blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(t.Context(), note); err == nil {
		t.Fatal("failed insert reported success")
	}
	if got, err := r.List(t.Context(), 100); err != nil || len(got) != 0 {
		t.Fatalf("failed write changed storage: %v %v", got, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(t.Context(), note); err == nil {
		t.Fatal("closed storage accepted write")
	}
	if got, err := r.List(t.Context(), 100); err == nil || got != nil {
		t.Fatal("closed storage returned success")
	}
}
