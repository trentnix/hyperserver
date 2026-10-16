package sqlite

import (
	"context"
	"errors"
	"testing"
)

func TestSessionHealth(t *testing.T) {
	s := setupSQLiteStore(t)
	if err := s.CheckHealth(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.CheckHealth(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), "DROP TABLE "+quoteIdentifier(s.tableName)); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckHealth(t.Context()); err == nil {
		t.Fatal("missing table reported healthy")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckHealth(t.Context()); err == nil {
		t.Fatal("closed store reported healthy")
	}
	if err := (&SQLiteStore{}).CheckHealth(t.Context()); err == nil {
		t.Fatal("uninitialized store reported healthy")
	}
}
