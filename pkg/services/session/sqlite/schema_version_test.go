package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	contacts "github.com/trentnix/hyperserver/modules/site/database"
	sqlitedb "github.com/trentnix/hyperserver/pkg/database/sqlite"
	accounts "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

func TestSessionVersionRejectsInvalidMetadata(t *testing.T) {
	for _, tc := range []struct{ name, change, want string }{
		{"missing", `DROP TABLE %s`, "unversioned"},
		{"view", `DROP TABLE %[1]s; CREATE VIEW %[1]s AS SELECT 1 AS version, 0 AS dirty`, "unversioned"},
		{"empty", `DELETE FROM %s`, "expected one version"},
		{"future", `UPDATE %s SET version = 2`, "version 2"},
		{"zero", `UPDATE %s SET version = 0`, "version 0"},
		{"negative", `UPDATE %s SET version = -1`, "version -1"},
		{"dirty", `UPDATE %s SET dirty = true`, "is dirty"},
		{"multiple", `INSERT INTO %s VALUES (2, false)`, "expected one version"},
		{"null version", `UPDATE %s SET version = NULL`, "expected one version"},
		{"null dirty", `UPDATE %s SET dirty = NULL`, "expected one version"},
		{"invalid dirty", `UPDATE %s SET dirty = 2`, "read SQLite session schema version"},
		{"invalid version", `UPDATE %s SET version = 'bad'`, "read SQLite session schema version"},
		{"malformed", `ALTER TABLE %s RENAME COLUMN dirty TO wrong_column`, "read SQLite session schema version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := setupSQLiteStore(t)
			original, request := savedSession(t, store)
			if _, err := store.db.ExecContext(t.Context(), fmt.Sprintf(tc.change, quoteIdentifier(sessionVersionTable(store.tableName)))); err != nil {
				t.Fatal(err)
			}
			before := versionStorageSnapshot(t, store.db)
			for range 2 {
				if err := store.configureDatabase(t.Context()); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("setup = %v, want %q", err, tc.want)
				}
			}
			if versionStorageSnapshot(t, store.db) != before {
				t.Fatal("failed initialization changed schema or version metadata")
			}
			loaded, err := store.Get(request, original.Name)
			if err != nil || loaded == nil || loaded.ID != original.ID || loaded.Data["identity"] != "account" {
				t.Fatalf("failed initialization changed session: %v, %v", loaded, err)
			}
		})
	}
}

func TestSessionVersionBootstrapRollsBack(t *testing.T) {
	for _, object := range []string{"sessions_expiration", sessionVersionTable("sessions")} {
		t.Run(object, func(t *testing.T) {
			db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "sessions.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			err = conn.Raw(func(raw any) error {
				raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(action int, name, _, _ string) int {
					if name == object && (action == sqlite3.SQLITE_CREATE_INDEX || action == sqlite3.SQLITE_INSERT) {
						return sqlite3.SQLITE_DENY
					}
					return sqlite3.SQLITE_OK
				})
				return nil
			})
			conn.Close()
			if err != nil {
				t.Fatal(err)
			}
			store := &SQLiteStore{db: db, tableName: "sessions"}
			if err := store.configureDatabase(t.Context()); err == nil {
				t.Fatal("denied bootstrap succeeded")
			}
			var count int
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial schema after failure: %d, %v", count, err)
			}
		})
	}
}

func TestProviderMigrationVersionsAreIndependent(t *testing.T) {
	if os.Getenv("HS_TEST_MIGRATE") == "" {
		t.Skip("set HS_TEST_MIGRATE to run migration CLI integration")
	}
	for _, bootstrap := range []string{"startup", "CLI"} {
		t.Run(bootstrap, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "shared database.db")
			db, err := sqlitedb.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if bootstrap == "startup" {
				if err := accounts.PrepareDatabase(t.Context(), db); err != nil {
					t.Fatal(err)
				}
				if err := contacts.PrepareDatabase(t.Context(), db); err != nil {
					t.Fatal(err)
				}
			}
			accountMigrations := "../../user/sqlite/migrations"
			contactMigrations := "../../../../modules/site/database/migrations"
			runProviderMigration(t, path, accountMigrations, "hyperserver_account_version", "up")
			runProviderMigration(t, path, contactMigrations, "hyperserver_contact_version", "up")
			if err := accounts.PrepareDatabase(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			if err := contacts.PrepareDatabase(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), `INSERT INTO user (id, email, email_key) VALUES ('preserved', 'a@example.invalid', 'A@EXAMPLE.INVALID');
				INSERT INTO hyperserver_contact_submission (id, email) VALUES ('preserved', 'a@example.invalid')`); err != nil {
				t.Fatal(err)
			}

			for _, table := range []string{"sessions", "admin sessions", `session"data`, `sessions"; DROP TABLE user; --`} {
				options := sqliteStoreOptions(path, table)
				if bootstrap == "startup" {
					store, err := NewSQLiteStore(t.Context(), options)
					if err != nil {
						t.Fatal(err)
					}
					store.Close()
				}
				up, down := sessionMigrationSQL(table)
				directory := t.TempDir()
				for name, sql := range map[string]string{"000001_sessions.up.sql": up, "000001_sessions.down.sql": down} {
					if err := os.WriteFile(filepath.Join(directory, name), []byte(sql), 0600); err != nil {
						t.Fatal(err)
					}
				}
				runProviderMigration(t, path, directory, sessionVersionTable(table), "up")
				store, err := NewSQLiteStore(t.Context(), options)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { store.Close() })
				original, request := savedSession(t, store)
				before := versionStorageSnapshot(t, db)
				runProviderMigration(t, path, directory, sessionVersionTable(table), "up")
				if err := store.configureDatabase(t.Context()); err != nil {
					t.Fatal(err)
				}
				if versionStorageSnapshot(t, db) != before {
					t.Fatal("repeated startup or migration changed version metadata")
				}
				loaded, err := store.Get(request, original.Name)
				if err != nil || loaded == nil || loaded.ID != original.ID {
					t.Fatalf("repeated startup changed session: %v, %v", loaded, err)
				}
				if table != "sessions" {
					runProviderMigration(t, path, directory, sessionVersionTable(table), "down", "1")
					if err := store.configureDatabase(t.Context()); err == nil {
						t.Fatal("startup recreated explicitly removed schema")
					}
					runProviderMigration(t, path, directory, sessionVersionTable(table), "up")
					if err := store.configureDatabase(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
			}

			runProviderMigration(t, path, contactMigrations, "hyperserver_contact_version", "down", "1")
			if err := contacts.PrepareDatabase(t.Context(), db); err == nil {
				t.Fatal("contact startup recreated explicitly removed schema")
			}
			for _, table := range []string{"user", "sessions"} {
				var count int
				if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+quoteIdentifier(table)).Scan(&count); err != nil || count != 1 {
					t.Fatalf("unrelated %s changed: %d, %v", table, count, err)
				}
			}
			runProviderMigration(t, path, contactMigrations, "hyperserver_contact_version", "up")
			if err := contacts.PrepareDatabase(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			// A dirty provider must not prevent another provider from checking its own version.
			if _, err := db.ExecContext(t.Context(), `UPDATE hyperserver_contact_version SET dirty = true`); err != nil {
				t.Fatal(err)
			}
			if err := contacts.PrepareDatabase(t.Context(), db); err == nil {
				t.Fatal("dirty contact schema accepted")
			}
			if err := accounts.PrepareDatabase(t.Context(), db); err != nil {
				t.Fatal("dirty contacts affected accounts:", err)
			}
		})
	}
}

func TestSessionVersionUsesSQLiteIdentifierCaseRules(t *testing.T) {
	options := sqliteStoreOptions(filepath.Join(t.TempDir(), "sessions.db"), "sessions")
	first, err := NewSQLiteStore(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	original, request := savedSession(t, first)
	options.Table = "SESSIONS"
	second, err := NewSQLiteStore(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	loaded, err := second.Get(request, original.Name)
	if err != nil || loaded == nil || loaded.ID != original.ID {
		t.Fatalf("case variant did not reuse the same session table: %v, %v", loaded, err)
	}
	var count int
	if err := first.db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE name LIKE 'hyperserver_session_version_%'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("version tables = %d, %v", count, err)
	}
}

func TestProviderMigrationFailureLeavesDirtyVersion(t *testing.T) {
	if os.Getenv("HS_TEST_MIGRATE") == "" {
		t.Skip("set HS_TEST_MIGRATE to run migration CLI integration")
	}
	for _, provider := range []string{"sessions", "contacts"} {
		t.Run(provider, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "failed.db")
			db, err := sqlitedb.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			up, _ := sessionMigrationSQL("sessions")
			versionTable := sessionVersionTable("sessions")
			if provider == "contacts" {
				data, err := os.ReadFile("../../../../modules/site/database/migrations/000001_contacts.up.sql")
				if err != nil {
					t.Fatal(err)
				}
				up, versionTable = string(data), "hyperserver_contact_version"
			}
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "000001_failure.up.sql"), []byte(up+"\nINSERT INTO missing_table VALUES (1);"), 0600); err != nil {
				t.Fatal(err)
			}
			if output, err := providerMigrationCommand(t, path, directory, versionTable, "up"); err == nil || !strings.Contains(output, "missing_table") {
				t.Fatalf("failed migration = %v, %s", err, output)
			}
			var count int
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE name IN ('sessions', 'sessions_expiration', 'hyperserver_contact_submission')`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("migration left partial schema: %d, %v", count, err)
			}
			before := versionStorageSnapshot(t, db)
			if provider == "contacts" {
				err = contacts.PrepareDatabase(t.Context(), db)
			} else {
				err = (&SQLiteStore{db: db, tableName: "sessions"}).configureDatabase(t.Context())
			}
			if err == nil || !strings.Contains(err.Error(), "is dirty") {
				t.Fatalf("startup accepted dirty migration: %v", err)
			}
			if versionStorageSnapshot(t, db) != before {
				t.Fatal("startup altered failed migration metadata")
			}
		})
	}
}

func runProviderMigration(t *testing.T, path, directory, versionTable string, command ...string) {
	t.Helper()
	if output, err := providerMigrationCommand(t, path, directory, versionTable, command...); err != nil {
		t.Fatalf("migration %v: %v\n%s", command, err, output)
	}
}

func providerMigrationCommand(t *testing.T, path, directory, versionTable string, command ...string) (string, error) {
	t.Helper()
	binary := os.Getenv("HS_TEST_MIGRATE")
	if binary == "" {
		t.Skip("set HS_TEST_MIGRATE to run migration CLI integration")
	}
	connection := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{
		"x-migrations-table": {quoteIdentifier(versionTable)}, "_foreign_keys": {"on"},
	}.Encode()}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	args := append([]string{"-path", directory, "-database", "sqlite3://" + connection.String()}, command...)
	output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	return string(output), err
}

func versionStorageSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var snapshot strings.Builder
	rows, err := db.QueryContext(t.Context(), `SELECT type, name, coalesce(sql, '') FROM sqlite_master ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var kind, name, definition string
		if err := rows.Scan(&kind, &name, &definition); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		fmt.Fprintf(&snapshot, "%s:%s:%s\n", kind, name, definition)
		if kind == "table" && strings.Contains(name, "version") {
			tables = append(tables, name)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		rows, err := db.QueryContext(t.Context(), "SELECT * FROM "+quoteIdentifier(table)+" ORDER BY 1")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var version, dirty any
			if err := rows.Scan(&version, &dirty); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			fmt.Fprintf(&snapshot, "%s:%v:%v\n", table, version, dirty)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return snapshot.String()
}
