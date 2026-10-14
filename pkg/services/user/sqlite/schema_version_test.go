package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
)

func TestAccountSchemaBootstrap(t *testing.T) {
	db := unpreparedUserTestDB(t)
	// Another provider's tables and version must not prevent fresh setup.
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE other_history (version INTEGER);
		INSERT INTO other_history VALUES (73)`); err != nil {
		t.Fatal(err)
	}
	if err := PrepareDatabase(t.Context(), db); err != nil {
		t.Fatal(err)
	}

	before := accountSchemaSnapshot(t, db)
	for range 2 {
		if err := PrepareDatabase(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	if after := accountSchemaSnapshot(t, db); after != before {
		t.Fatal("repeated setup changed schema or version metadata")
	}
	var count, version int
	var dirty bool
	if err := db.QueryRowContext(t.Context(), `SELECT count(*), min(version), min(dirty) FROM hyperserver_account_version`).Scan(&count, &version, &dirty); err != nil || count != 1 || version != 1 || dirty {
		t.Fatalf("version metadata = (%d, %d, %t), %v", count, version, dirty, err)
	}
	var otherVersion int
	if err := db.QueryRowContext(t.Context(), `SELECT version FROM other_history`).Scan(&otherVersion); err != nil || otherVersion != 73 {
		t.Fatalf("other provider version changed: %d, %v", otherVersion, err)
	}
}

func TestAccountSchemaRejectsUnversionedStorage(t *testing.T) {
	for _, tc := range []struct{ name, schema string }{
		{"complete schema", accountSchemaV1},
		{"account table only", accountKeyTestSchema},
		{"token table only", tokenKeyTestSchema},
		{"account view", `CREATE VIEW user AS SELECT 'preserved' AS value`},
		{"uppercase name", `CREATE TABLE USER (value TEXT); INSERT INTO USER VALUES ('preserved')`},
		{"index conflict", `CREATE TABLE unrelated (value TEXT); CREATE INDEX usertoken_expiration ON unrelated(value)`},
		{"history view", `CREATE VIEW hyperserver_account_version AS SELECT 1 AS version`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := unpreparedUserTestDB(t)
			if _, err := db.ExecContext(t.Context(), tc.schema); err != nil {
				t.Fatal(err)
			}
			assertAccountSetupRejected(t, db, "unversioned SQLite account storage")
		})
	}
}

func TestAccountFreshSetupRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action int
		object string
	}{
		{"token table", sqlite3.SQLITE_CREATE_TABLE, "usertoken"},
		{"expiration index", sqlite3.SQLITE_CREATE_INDEX, "usertoken_expiration"},
		{"version insert", sqlite3.SQLITE_INSERT, accountVersionTable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := unpreparedUserTestDB(t)
			setAuthorizer := func(callback func(int, string, string, string) int) {
				t.Helper()
				conn, err := db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if err := conn.Raw(func(raw any) error {
					raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(callback)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			setAuthorizer(func(action int, object, _, _ string) int {
				if action == tc.action && object == tc.object {
					return sqlite3.SQLITE_DENY
				}
				return sqlite3.SQLITE_OK
			})
			if err := PrepareDatabase(t.Context(), db); err == nil {
				t.Fatal("setup succeeded despite denied schema write")
			}
			assertNoAccountTables(t, db)
			setAuthorizer(nil)
			if err := PrepareDatabase(t.Context(), db); err != nil {
				t.Fatal("retry after rollback:", err)
			}
		})
	}
}

func TestAccountSchemaRejectsUnsupportedHistory(t *testing.T) {
	for _, tc := range []struct {
		name, change, want string
	}{
		{"missing history", `DROP TABLE hyperserver_account_version`, "unversioned"},
		{"empty history", `DELETE FROM hyperserver_account_version`, "expected one version and dirty flag"},
		{"version zero", `UPDATE hyperserver_account_version SET version = 0`, "unsupported SQLite account schema version 0"},
		{"future version", `UPDATE hyperserver_account_version SET version = 2`, "unsupported SQLite account schema version 2"},
		{"negative version", `UPDATE hyperserver_account_version SET version = -1`, "unsupported SQLite account schema version -1"},
		{"dirty baseline", `UPDATE hyperserver_account_version SET dirty = true`, "version 1 is dirty"},
		{"dirty future version", `UPDATE hyperserver_account_version SET version = 2, dirty = true`, "version 2 is dirty"},
		{"multiple versions", `INSERT INTO hyperserver_account_version VALUES (2, false)`, "expected one version and dirty flag"},
		{"duplicate version", `INSERT INTO hyperserver_account_version VALUES (1, false)`, "expected one version and dirty flag"},
		{"null version", `UPDATE hyperserver_account_version SET version = NULL`, "expected one version and dirty flag"},
		{"null dirty flag", `UPDATE hyperserver_account_version SET dirty = NULL`, "expected one version and dirty flag"},
		{"invalid version", `UPDATE hyperserver_account_version SET version = 'invalid'`, "read account schema version"},
		{"invalid dirty flag", `UPDATE hyperserver_account_version SET dirty = 2`, "read account schema version"},
		{"malformed history", `ALTER TABLE hyperserver_account_version RENAME COLUMN dirty TO wrong_column`, "read account schema version"},
		{"missing token table", `DROP TABLE usertoken`, "missing usertoken.user_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := userTestDB(t)
			if _, err := db.ExecContext(t.Context(), `INSERT INTO user (id, email, email_key) VALUES ('preserved', 'person@example.invalid', 'person@example.invalid');`+tc.change); err != nil {
				t.Fatal(err)
			}
			assertAccountSetupRejected(t, db, tc.want)
			var email string
			if err := db.QueryRowContext(t.Context(), `SELECT email FROM user WHERE id = 'preserved'`).Scan(&email); err != nil || email != "person@example.invalid" {
				t.Fatalf("failed setup changed account: %q, %v", email, err)
			}
		})
	}
}

func TestAccountVersionCheckIsReadOnly(t *testing.T) {
	db := userTestDB(t)
	if _, err := db.ExecContext(t.Context(), `PRAGMA query_only = ON`); err != nil {
		t.Fatal(err)
	}
	if err := PrepareDatabase(t.Context(), db); err != nil {
		t.Fatal("versioned storage required a write:", err)
	}
}

func assertAccountSetupRejected(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	before := accountSchemaSnapshot(t, db)
	for range 2 {
		if err := PrepareDatabase(t.Context(), db); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("setup error = %v, want %q", err, want)
		}
	}
	if after := accountSchemaSnapshot(t, db); after != before {
		t.Fatal("rejected setup changed schema or migration history")
	}
}

// accountSchemaSnapshot includes the exact history rows, even for malformed
// history fixtures, so rejected startup cannot silently relabel existing data.
func accountSchemaSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var snapshot strings.Builder
	for _, query := range []string{
		`SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY name`,
		`SELECT * FROM hyperserver_account_version ORDER BY 1`,
	} {
		rows, err := db.QueryContext(t.Context(), query)
		if err != nil {
			if strings.Contains(err.Error(), "no such table: hyperserver_account_version") {
				continue
			}
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
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
