package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	sqlitedb "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

func TestSQLiteStartupRejectsMissingSessionColumns(t *testing.T) {
	for _, table := range []string{"sessions", `session"data`} {
		for _, column := range []string{"id", "session", "expires_at", "created_at", "updated_at"} {
			t.Run(table+"/"+column, func(t *testing.T) {
				options := sqliteStoreOptions(filepath.Join(t.TempDir(), "sessions.db"), table)
				store, err := NewSQLiteStore(t.Context(), options)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { store.Close() })
				original, request := savedSession(t, store)

				readRecord := func() SQLiteSession {
					t.Helper()
					var record SQLiteSession
					query := "SELECT id, session, expires_at, created_at, updated_at FROM " + quoteIdentifier(table)
					if err := store.db.QueryRowContext(t.Context(), query).Scan(&record.ID, &record.Session, &record.Expires_at, &record.Created_at, &record.Updated_at); err != nil {
						t.Fatal(err)
					}
					return record
				}
				before := readRecord()
				query := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO legacy_column", quoteIdentifier(table), quoteIdentifier(column))
				if _, err := store.db.ExecContext(t.Context(), query); err != nil {
					t.Fatal(err)
				}
				if _, err := store.db.ExecContext(t.Context(), "DROP INDEX "+quoteIdentifier(table+"_expiration")); err != nil {
					t.Fatal(err)
				}
				var schemaBefore string
				if err := store.db.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_master WHERE name = ?", table).Scan(&schemaBefore); err != nil {
					t.Fatal(err)
				}

				for range 2 {
					opened, err := NewSQLiteStore(t.Context(), options)
					if opened != nil {
						opened.Close()
						t.Fatal("incompatible schema returned a store")
					}
					if err == nil || !strings.Contains(err.Error(), "check SQLite session schema columns") || !strings.Contains(err.Error(), column) {
						t.Fatalf("missing column accepted or poorly identified: %v", err)
					}
					if strings.Contains(err.Error(), original.ID) || strings.Contains(err.Error(), before.Session) {
						t.Fatal("schema error exposed stored session data")
					}
				}
				var indexCount int
				if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name = ?", table+"_expiration").Scan(&indexCount); err != nil || indexCount != 0 {
					t.Fatalf("failed startup created the expiration index: %d, %v", indexCount, err)
				}
				var schemaAfter string
				if err := store.db.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_master WHERE name = ?", table).Scan(&schemaAfter); err != nil || schemaAfter != schemaBefore {
					t.Fatalf("failed startup changed the table: %q, %v", schemaAfter, err)
				}

				// Repair explicitly, then verify that startup preserves the saved session.
				query = fmt.Sprintf("ALTER TABLE %s RENAME COLUMN legacy_column TO %s", quoteIdentifier(table), quoteIdentifier(column))
				if _, err := store.db.ExecContext(t.Context(), query); err != nil {
					t.Fatal(err)
				}
				reopened, err := NewSQLiteStore(t.Context(), options)
				if err != nil {
					t.Fatal("retry after repair:", err)
				}
				t.Cleanup(func() { reopened.Close() })
				if after := readRecord(); after != before {
					t.Fatal("startup changed the stored session")
				}
				loaded, err := reopened.Get(request, original.Name)
				if err != nil || loaded == nil || loaded.IsNew || loaded.ID != original.ID || loaded.Data["identity"] != "account" {
					t.Fatalf("session did not survive repair and reopen: %+v, %v", loaded, err)
				}
			})
		}
	}
}

func TestSQLiteSchemaCheckDoesNotReadPayloads(t *testing.T) {
	store := setupSQLiteStore(t)
	original, request := savedSession(t, store)
	if _, err := store.db.ExecContext(t.Context(), "UPDATE sessions_test SET session = 'invalid encoded payload'"); err != nil {
		t.Fatal(err)
	}
	if err := store.configureDatabase(t.Context()); err != nil {
		t.Fatal("schema inspection read stored payloads:", err)
	}
	var payload string
	if err := store.db.QueryRowContext(t.Context(), "SELECT session FROM sessions_test").Scan(&payload); err != nil || payload != "invalid encoded payload" {
		t.Fatalf("schema inspection changed the payload: %q, %v", payload, err)
	}
	if _, err := store.Get(request, original.Name); err == nil {
		t.Fatal("reading a corrupt session must still fail")
	}
}

func TestSQLiteSchemaAllowsReorderedAndExtraColumns(t *testing.T) {
	options := sqliteStoreOptions(filepath.Join(t.TempDir(), "sessions.db"), "sessions")
	db, err := sqlitedb.Open(options.Connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE sessions (
		extra TEXT, UPDATED_AT DATETIME, SESSION TEXT, ID VARCHAR(36) PRIMARY KEY,
		CREATED_AT DATETIME, EXPIRES_AT DATETIME
	)`); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(t.Context(), options)
	if err != nil {
		t.Fatal("compatible schema rejected:", err)
	}
	t.Cleanup(func() { store.Close() })
	original, request := savedSession(t, store)
	loaded, err := store.Get(request, original.Name)
	if err != nil || loaded == nil || loaded.IsNew || loaded.ID != original.ID || loaded.Data["identity"] != "account" {
		t.Fatalf("compatible schema round trip failed: %+v, %v", loaded, err)
	}
}

func TestSQLiteSchemaBootstrapRollback(t *testing.T) {
	options := sqliteStoreOptions(filepath.Join(t.TempDir(), "sessions.db"), "sessions")
	db, err := sqlitedb.Open(options.Connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// A table with the expiration index's name makes the final setup step fail.
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE sessions_expiration (value TEXT); INSERT INTO sessions_expiration VALUES ('preserved')"); err != nil {
		t.Fatal(err)
	}
	opened, err := NewSQLiteStore(t.Context(), options)
	if opened != nil {
		opened.Close()
		t.Fatal("failed setup returned a store")
	}
	if err == nil {
		t.Fatal("conflicting expiration index name accepted")
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name = 'sessions'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed setup left the session table: %d, %v", count, err)
	}
	var value string
	if err := db.QueryRowContext(t.Context(), "SELECT value FROM sessions_expiration").Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("failed setup changed unrelated data: %q, %v", value, err)
	}
	if _, err := db.ExecContext(t.Context(), "ALTER TABLE sessions_expiration RENAME TO unrelated_data"); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(t.Context(), options)
	if err != nil {
		t.Fatal("retry after repair:", err)
	}
	t.Cleanup(func() { store.Close() })
}

func TestSQLiteSchemaCheckCancellation(t *testing.T) {
	store := setupSQLiteStore(t)
	original, request := savedSession(t, store)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.configureDatabase(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("setup lost cancellation: %v", err)
	}
	if err := store.configureDatabase(t.Context()); err != nil {
		t.Fatal("retry after cancellation:", err)
	}
	loaded, err := store.Get(request, original.Name)
	if err != nil || loaded == nil || loaded.IsNew || loaded.ID != original.ID {
		t.Fatalf("canceled setup changed the session: %+v, %v", loaded, err)
	}
}
