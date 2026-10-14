package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAccountMigrationCompatibility(t *testing.T) {
	for _, bootstrap := range []string{"provider", "migration"} {
		t.Run(bootstrap, func(t *testing.T) {
			db := unpreparedUserTestDB(t)
			run := migrationCommand(t, db, "migrations", accountVersionTable)
			if bootstrap == "provider" {
				if err := PrepareDatabase(t.Context(), db); err != nil {
					t.Fatal(err)
				}
			}
			runMigration(t, run, "up")
			if err := PrepareDatabase(t.Context(), db); err != nil {
				t.Fatal(err)
			}

			repo := NewAccountRepository(db)
			account := &accounts.User{Email: "person@example.invalid"}
			if err := repo.Create(t.Context(), account); err != nil {
				t.Fatal(err)
			}
			before := accountSchemaSnapshot(t, db)
			for range 2 {
				runMigration(t, run, "up")
				if err := PrepareDatabase(t.Context(), db); err != nil {
					t.Fatal(err)
				}
			}
			if after := accountSchemaSnapshot(t, db); after != before {
				t.Fatal("repeated migration or startup changed schema or version")
			}
			got, err := repo.GetByEmail(t.Context(), account.Email)
			if err != nil || got == nil || got.ID != account.ID {
				t.Fatalf("account not preserved: %v, %v", got, err)
			}
		})
	}
}

func TestAccountMigrationEscapedPaths(t *testing.T) {
	for _, filename := range []string{"with spaces.db", "literal%20.db", "hash#name.db", "query?name.db", "unicode-é.db"} {
		t.Run(filename, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "account storage")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			connection := url.URL{Scheme: "file", Path: filepath.Join(directory, filename)}
			db, err := sql.Open("sqlite3", connection.String())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			run := migrationCommand(t, db, "migrations", accountVersionTable)
			runMigration(t, run, "up")

			// Check the original connection before startup can bootstrap a schema.
			var version int
			var dirty bool
			if err := db.QueryRowContext(t.Context(), `SELECT version, dirty FROM hyperserver_account_version`).Scan(&version, &dirty); err != nil || version != 1 || dirty {
				t.Fatalf("migration did not update the intended database: %d, %t, %v", version, dirty, err)
			}
			if err := PrepareDatabase(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			runMigration(t, run, "down", "1")
			assertAccountSetupRejected(t, db, "expected one version and dirty flag")

			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 || entries[0].Name() != filename {
				t.Fatalf("migration created unexpected files: %v, %v", entries, err)
			}
		})
	}
}

func TestAccountMigrationMissingExecutable(t *testing.T) {
	t.Setenv("HS_TEST_MIGRATE", "migrate")
	t.Setenv("PATH", t.TempDir())
	run := migrationCommand(t, unpreparedUserTestDB(t), "migrations", accountVersionTable)
	if output, err := run("up"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing migration executable: %v, %s", err, output)
	}
}

func TestAccountMigrationDownAndIndependentVersions(t *testing.T) {
	db := unpreparedUserTestDB(t)
	run := migrationCommand(t, db, "migrations", accountVersionTable)
	otherDirectory := writeMigrationFiles(t, map[string]string{
		"000073_other.up.sql":   `CREATE TABLE other_data (value TEXT); INSERT INTO other_data VALUES ('preserved');`,
		"000073_other.down.sql": `DROP TABLE other_data;`,
	})
	other := migrationCommand(t, db, otherDirectory, "other_version")
	runMigration(t, other, "up")
	runMigration(t, run, "up")
	if _, err := db.ExecContext(t.Context(), `INSERT INTO user (id, email, email_key) VALUES ('account', 'person@example.invalid', 'person@example.invalid');
		INSERT INTO usertoken (user_id, token_hash, expires_at, token_type) VALUES ('account', 'hash', '2030-01-01', 'reset')`); err != nil {
		t.Fatal(err)
	}

	runMigration(t, run, "down", "1")
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE name IN ('user', 'usertoken', 'usertoken_expiration')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("down left account objects: %d, %v", count, err)
	}
	assertAccountSetupRejected(t, db, "expected one version and dirty flag")
	runMigration(t, run, "up")
	if err := PrepareDatabase(t.Context(), db); err != nil {
		t.Fatal(err)
	}

	var version int
	var dirty bool
	if err := db.QueryRowContext(t.Context(), `SELECT version, dirty FROM other_version`).Scan(&version, &dirty); err != nil || version != 73 || dirty {
		t.Fatalf("other version changed: %d, %t, %v", version, dirty, err)
	}
	var value string
	if err := db.QueryRowContext(t.Context(), `SELECT value FROM other_data`).Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("other data changed: %q, %v", value, err)
	}
	before := accountSchemaSnapshot(t, db)
	runMigration(t, other, "down", "1")
	if err := PrepareDatabase(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	runMigration(t, other, "up")
	if after := accountSchemaSnapshot(t, db); after != before {
		t.Fatal("other migrations changed account schema or metadata")
	}
}

func TestAccountMigrationFailure(t *testing.T) {
	for _, initial := range []bool{true, false} {
		name := "upgrade"
		if initial {
			name = "initial"
		}
		t.Run(name, func(t *testing.T) {
			db := unpreparedUserTestDB(t)
			valid := migrationCommand(t, db, "migrations", accountVersionTable)
			files := map[string]string{
				"000001_accounts.up.sql": accountSchemaV1 + "\nINSERT INTO missing_table VALUES (1);",
			}
			wantDirty := "version 1 is dirty"
			if !initial {
				runMigration(t, valid, "up")
				if _, err := db.ExecContext(t.Context(), `INSERT INTO user (id, email, email_key) VALUES ('preserved', 'person@example.invalid', 'person@example.invalid')`); err != nil {
					t.Fatal(err)
				}
				files["000001_accounts.up.sql"] = accountSchemaV1
				files["000002_failure.up.sql"] = `CREATE TABLE partial_change (value TEXT);
					DELETE FROM user; INSERT INTO missing_table VALUES (1);`
				wantDirty = "version 2 is dirty"
			}
			failing := migrationCommand(t, db, writeMigrationFiles(t, files), accountVersionTable)
			if output, err := failing("up"); err == nil || !strings.Contains(output, "missing_table") {
				t.Fatalf("invalid migration = %v, %s", err, output)
			}
			assertAccountSetupRejected(t, db, wantDirty)
			if output, err := valid("up"); err == nil || !strings.Contains(output, "Dirty database") {
				t.Fatalf("dirty migration was not rejected: %v, %s", err, output)
			}

			var count int
			if initial {
				if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE name IN ('user', 'usertoken', 'usertoken_expiration')`).Scan(&count); err != nil || count != 0 {
					t.Fatalf("initial migration left partial schema: %d, %v", count, err)
				}
				// The failed transaction left no account objects. Explicitly reset
				// the marker before retrying, never as part of application startup.
				runMigration(t, valid, "force", "--", "-1")
				runMigration(t, valid, "up")
			} else {
				if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE name = 'partial_change'`).Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed upgrade left partial schema: %d, %v", count, err)
				}
				if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM user WHERE id = 'preserved'`).Scan(&count); err != nil || count != 1 {
					t.Fatalf("failed upgrade changed account data: %d, %v", count, err)
				}
				// Both schema and data rolled back to version 1.
				runMigration(t, valid, "force", "1")
			}
			if err := PrepareDatabase(t.Context(), db); err != nil {
				t.Fatal("explicit repair did not restore usable storage:", err)
			}
		})
	}
}

// migrationCommand runs the separately installed CLI. CI always supplies it.
// Keeping the tool out of the module avoids linking its dependencies into tests.
func migrationCommand(t *testing.T, db *sql.DB, directory, table string) func(...string) (string, error) {
	t.Helper()
	binary := os.Getenv("HS_TEST_MIGRATE")
	if binary == "" {
		t.Skip("set HS_TEST_MIGRATE to the golang-migrate v4.20.1 executable")
	}
	var sequence int
	var name, path string
	if err := db.QueryRowContext(t.Context(), `PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	// A file URI lets SQLite decode escaped characters in the actual filename.
	connection := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{
		"x-migrations-table": {table},
		"_foreign_keys":      {"on"},
	}.Encode()}
	return func(command ...string) (string, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		args := append([]string{"-path", directory, "-database", "sqlite3://" + connection.String()}, command...)
		output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
		return string(output), err
	}
}

func runMigration(t *testing.T, run func(...string) (string, error), args ...string) {
	t.Helper()
	if output, err := run(args...); err != nil {
		t.Fatalf("migrate %v: %v\n%s", args, err, output)
	}
}

func writeMigrationFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	directory := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}
