package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
)

func TestSetupAccountStorageDisabled(t *testing.T) {
	s := &server.ApplicationServer{Config: &config.Config{}}
	if err := setupAccountStorage(context.Background(), s); err != nil {
		t.Fatal("disabled auth required a database:", err)
	}

	s.Database = accountSetupTestDB(t)
	if err := setupAccountStorage(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.Database.Get(&count, `SELECT count(*) FROM sqlite_master WHERE type = 'table'`); err != nil || count != 0 {
		t.Fatalf("disabled auth created tables: count = %d, error = %v", count, err)
	}
}

func TestSetupAccountStorageDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{accountSetupTimeout, time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				db := accountSetupTestDB(t)
				// Hold the only connection so setup must wait for its deadline.
				conn, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				ctx := context.Background()
				if timeout < accountSetupTimeout {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, timeout)
					defer cancel()
				}
				s := &server.ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{Enabled: true}}, Database: db}
				start := time.Now()
				if err := setupAccountStorage(ctx, s); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("setup deadline error = %v", err)
				}
				if elapsed := time.Since(start); elapsed != timeout {
					t.Fatalf("setup waited %s, want %s", elapsed, timeout)
				}
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
				if err := setupAccountStorage(context.Background(), s); err != nil {
					t.Fatal("retry after deadline:", err)
				}
			})
		})
	}
}

func accountSetupTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}
