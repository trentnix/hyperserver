package storage

import (
	"context"
	"errors"
	"testing"
)

func TestSQLiteContactHealth(t *testing.T) {
	db := contactTestDB(t)
	repository, err := NewSQLiteContactRepository(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CheckHealth(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := repository.CheckHealth(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "DROP TABLE hyperserver_contact_submission"); err != nil {
		t.Fatal(err)
	}
	if err := repository.CheckHealth(t.Context()); err == nil {
		t.Fatal("missing table reported healthy")
	}
}
