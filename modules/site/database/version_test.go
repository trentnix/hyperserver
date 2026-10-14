package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
)

func contactVersionTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestContactVersionRejectsInvalidMetadata(t *testing.T) {
	for _, tc := range []struct{ name, change, want string }{
		{"missing", `DROP TABLE hyperserver_contact_version`, "unversioned"},
		{"view", `DROP TABLE hyperserver_contact_version; CREATE VIEW hyperserver_contact_version AS SELECT 1 AS version, 0 AS dirty`, "unversioned"},
		{"empty", `DELETE FROM hyperserver_contact_version`, "expected one version"},
		{"future", `UPDATE hyperserver_contact_version SET version = 2`, "version 2"},
		{"zero", `UPDATE hyperserver_contact_version SET version = 0`, "version 0"},
		{"negative", `UPDATE hyperserver_contact_version SET version = -1`, "version -1"},
		{"dirty", `UPDATE hyperserver_contact_version SET dirty = true`, "is dirty"},
		{"multiple", `INSERT INTO hyperserver_contact_version VALUES (2, false)`, "expected one version"},
		{"null version", `UPDATE hyperserver_contact_version SET version = NULL`, "expected one version"},
		{"null dirty", `UPDATE hyperserver_contact_version SET dirty = NULL`, "expected one version"},
		{"invalid dirty", `UPDATE hyperserver_contact_version SET dirty = 2`, "read SQLite contact schema version"},
		{"invalid version", `UPDATE hyperserver_contact_version SET version = 'bad'`, "read SQLite contact schema version"},
		{"malformed", `ALTER TABLE hyperserver_contact_version RENAME COLUMN dirty TO wrong_column`, "read SQLite contact schema version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := contactVersionTestDB(t)
			if err := PrepareDatabase(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), `INSERT INTO hyperserver_contact_submission (id, email, message)
				VALUES ('preserved', 'a@example.invalid', 'preserved');`+tc.change); err != nil {
				t.Fatal(err)
			}
			before := contactVersionSnapshot(t, db)
			for range 2 {
				if err := PrepareDatabase(t.Context(), db); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("setup = %v, want %q", err, tc.want)
				}
			}
			if contactVersionSnapshot(t, db) != before {
				t.Fatal("failed setup changed schema, version, or data")
			}
		})
	}
}

func TestContactVersionReadOnly(t *testing.T) {
	db := contactVersionTestDB(t)
	if err := PrepareDatabase(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `PRAGMA query_only = ON`); err != nil {
		t.Fatal(err)
	}
	if err := PrepareDatabase(t.Context(), db); err != nil {
		t.Fatal("version check attempted a write:", err)
	}
}

func TestContactVersionBootstrapRollback(t *testing.T) {
	db := contactVersionTestDB(t)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(action int, object, _, _ string) int {
			if action == sqlite3.SQLITE_INSERT && object == contactVersionTable {
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
	if err := PrepareDatabase(t.Context(), db); err == nil {
		t.Fatal("denied metadata insertion succeeded")
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial schema survived rollback: %d, %v", count, err)
	}
}

func contactVersionSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var snapshot strings.Builder
	for _, query := range []string{
		`SELECT type, name, coalesce(sql, '') FROM sqlite_master ORDER BY name`,
		`SELECT * FROM hyperserver_contact_version ORDER BY 1`,
		`SELECT id, email, message FROM hyperserver_contact_submission ORDER BY id`,
	} {
		rows, err := db.QueryContext(t.Context(), query)
		if err != nil {
			if strings.Contains(err.Error(), "no such table: hyperserver_contact_version") {
				continue
			}
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		values, pointers := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		for rows.Next() {
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			fmt.Fprintf(&snapshot, "%v\n", values)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return snapshot.String()
}
