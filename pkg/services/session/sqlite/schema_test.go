package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	sqlitedb "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

const sessionSchemaFixture = `CREATE TABLE sessions (
	id VARCHAR(36) PRIMARY KEY,
	session TEXT,
	expires_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`

func sessionSchemaDB(t *testing.T, schema string) (*sql.DB, Options) {
	t.Helper()
	options := sqliteStoreOptions(filepath.Join(t.TempDir(), "sessions.db"), "sessions")
	db, err := sqlitedb.Open(options.Connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(t.Context(), schema); err != nil {
		t.Fatal(err)
	}
	versionSessionFixture(t, db, options.Table)
	return db, options
}

// Supply version metadata so declaration tests reach the schema checks.
func versionSessionFixture(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	name := quoteIdentifier(sessionVersionTable(table))
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE "+name+" (version uint64, dirty bool); INSERT INTO "+name+" VALUES (1, false)"); err != nil {
		t.Fatal(err)
	}
}

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
	versionSessionFixture(t, db, options.Table)
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

func TestSQLiteSchemaNameConflictAndRetry(t *testing.T) {
	options := sqliteStoreOptions(filepath.Join(t.TempDir(), "sessions.db"), "sessions")
	db, err := sqlitedb.Open(options.Connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// An unrelated table occupies the expiration index's name.
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

func TestSQLiteSessionColumnDeclarations(t *testing.T) {
	for _, column := range []struct{ name, declaredType string }{
		{"id", "VARCHAR(36)"},
		{"session", "TEXT"},
		{"expires_at", "DATETIME"},
		{"created_at", "DATETIME"},
		{"updated_at", "DATETIME"},
	} {
		types := []string{"BLOB", "INTEGER", "NUMERIC", "CHARINT", ""}
		if column.declaredType == "DATETIME" {
			types = append(types, "TEXT", "DATETIME(6)", `" DATETIME "`)
		}
		for _, declaredType := range types {
			t.Run(column.name+"/"+declaredType, func(t *testing.T) {
				schema := strings.Replace(sessionSchemaFixture, column.name+" "+column.declaredType, column.name+" "+declaredType, 1)
				_, options := sessionSchemaDB(t, schema)
				store, err := NewSQLiteStore(t.Context(), options)
				if store != nil {
					store.Close()
					t.Fatal("incompatible type returned a store")
				}
				if err == nil || !strings.Contains(err.Error(), column.name+" requires a supported") {
					t.Fatalf("incompatible type accepted or poorly identified: %v", err)
				}
			})
		}
		for _, storage := range []string{"VIRTUAL", "STORED"} {
			t.Run(column.name+"/generated/"+storage, func(t *testing.T) {
				// SQLite permits a generated unique key, but not a generated primary key.
				schema := strings.Replace(sessionSchemaFixture, "PRIMARY KEY", "UNIQUE", 1)
				declaration := column.name + " " + column.declaredType
				schema = strings.Replace(schema, declaration+" DEFAULT CURRENT_TIMESTAMP", declaration, 1)
				schema = strings.Replace(schema, declaration, declaration+" GENERATED ALWAYS AS ('fixed') "+storage, 1)
				_, options := sessionSchemaDB(t, schema)
				store, err := NewSQLiteStore(t.Context(), options)
				if store != nil {
					store.Close()
					t.Fatal("generated column returned a store")
				}
				if err == nil || !strings.Contains(err.Error(), column.name+" must be an ordinary writable column") {
					t.Fatalf("generated column accepted: %v", err)
				}
			})
		}
	}
}

func TestSQLiteSessionUniqueKeyDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name, key, index string
		wantError        bool
	}{
		{name: "primary key", key: "PRIMARY KEY"},
		{name: "unique constraint", key: "UNIQUE"},
		{name: "explicit index", index: `CREATE UNIQUE INDEX "session ID" ON sessions(ID DESC)`},
		{name: "missing", wantError: true},
		{name: "ordinary index", index: "CREATE INDEX session_id ON sessions(id)", wantError: true},
		{name: "partial", index: "CREATE UNIQUE INDEX session_id ON sessions(id) WHERE session IS NOT NULL", wantError: true},
		{name: "composite", index: "CREATE UNIQUE INDEX session_id ON sessions(id, expires_at)", wantError: true},
		{name: "expression", index: "CREATE UNIQUE INDEX session_id ON sessions(lower(id))", wantError: true},
		{name: "extra expression", index: "CREATE UNIQUE INDEX session_id ON sessions(id, lower(id))", wantError: true},
		{name: "nonbinary", index: "CREATE UNIQUE INDEX session_id ON sessions(id COLLATE NOCASE)", wantError: true},
		{name: "wrong column", index: "CREATE UNIQUE INDEX session_id ON sessions(session)", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := strings.Replace(sessionSchemaFixture, "PRIMARY KEY", tc.key, 1)
			db, options := sessionSchemaDB(t, schema)
			if tc.index != "" {
				if _, err := db.ExecContext(t.Context(), tc.index); err != nil {
					t.Fatal(err)
				}
			}
			store, err := NewSQLiteStore(t.Context(), options)
			if tc.wantError {
				if store != nil {
					store.Close()
					t.Fatal("incompatible key returned a store")
				}
				if err == nil || !strings.Contains(err.Error(), "id requires a non-partial unique key with BINARY comparison") {
					t.Fatalf("incompatible key accepted or poorly identified: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal("compatible key rejected:", err)
			}
			t.Cleanup(func() { store.Close() })
			original, request := savedSession(t, store)
			if _, err := db.ExecContext(t.Context(), "INSERT INTO sessions SELECT * FROM sessions"); err == nil {
				t.Fatal("duplicate session ID accepted")
			}
			loaded, err := store.Get(request, original.Name)
			if err != nil || loaded == nil || loaded.ID != original.ID || loaded.Data["identity"] != "account" {
				t.Fatalf("duplicate insert changed existing session: %+v, %v", loaded, err)
			}
		})
	}
}

func TestSQLiteCompatibleSessionTypesRoundTrip(t *testing.T) {
	for _, timestamp := range []string{"DATE", "datetime", "TIMESTAMP"} {
		for _, suffix := range []string{"", " WITHOUT ROWID"} {
			t.Run(timestamp+suffix, func(t *testing.T) {
				schema := strings.NewReplacer(
					"VARCHAR(36)", "CLOB NOT NULL",
					"session TEXT", "session NCHAR(100) NOT NULL",
					"DATETIME", timestamp+" NOT NULL",
				).Replace(sessionSchemaFixture) + suffix
				_, options := sessionSchemaDB(t, schema)
				store, err := NewSQLiteStore(t.Context(), options)
				if err != nil {
					t.Fatal("compatible schema rejected:", err)
				}
				t.Cleanup(func() { store.Close() })
				original, request := savedSession(t, store)
				loaded, err := store.Get(request, original.Name)
				if err != nil || loaded == nil || loaded.IsNew || loaded.ID != original.ID || loaded.Data["identity"] != "account" || !loaded.ExpiresAt.Equal(original.ExpiresAt) {
					t.Fatalf("session round trip failed: %+v, %v", loaded, err)
				}
				loaded.Data["identity"] = "updated"
				if err := loaded.Save(httptest.NewRecorder(), request); err != nil {
					t.Fatal(err)
				}
				updated, err := store.Get(request, original.Name)
				if err != nil || updated == nil || updated.Data["identity"] != "updated" || !updated.ExpiresAt.Equal(original.ExpiresAt) {
					t.Fatalf("updated session round trip failed: %+v, %v", updated, err)
				}
			})
		}
	}
}

func TestSQLiteSessionSchemaFailurePreservesDuplicates(t *testing.T) {
	db, options := sessionSchemaDB(t, strings.Replace(sessionSchemaFixture, "PRIMARY KEY", "", 1))
	if _, err := db.ExecContext(t.Context(), `INSERT INTO sessions (id, session) VALUES ('duplicate', 'first'), ('duplicate', 'second')`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		store, err := NewSQLiteStore(t.Context(), options)
		if store != nil {
			store.Close()
			t.Fatal("missing uniqueness returned a store")
		}
		if err == nil {
			t.Fatal("missing uniqueness accepted")
		}
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM sessions WHERE id = 'duplicate' AND session IN ('first', 'second')").Scan(&count); err != nil || count != 2 {
		t.Fatalf("startup changed duplicate records: %d, %v", count, err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name = 'sessions_expiration'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed startup left an expiration index: %d, %v", count, err)
	}

	// Resolve the duplicate and supply the required key through explicit repair.
	if _, err := db.ExecContext(t.Context(), `UPDATE sessions SET id = 'distinct' WHERE session = 'second';
		CREATE UNIQUE INDEX session_id ON sessions(id)`); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(t.Context(), options)
	if err != nil {
		t.Fatal("retry after repair:", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM sessions WHERE session IN ('first', 'second')").Scan(&count); err != nil || count != 2 {
		t.Fatalf("startup changed repaired records: %d, %v", count, err)
	}
}

func TestSQLiteSessionSchemaInspectionFailures(t *testing.T) {
	for _, failure := range []string{"canceled", "finished transaction"} {
		t.Run(failure, func(t *testing.T) {
			store := setupSQLiteStore(t)
			tx, err := store.db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := context.Canceled
			if failure == "canceled" {
				cancel()
			} else {
				want = sql.ErrTxDone
				if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}
			}
			if err := validateSessionSchema(ctx, tx, store.tableName); !errors.Is(err, want) {
				t.Fatalf("schema inspection lost its underlying error: %v", err)
			}
		})
	}
}
