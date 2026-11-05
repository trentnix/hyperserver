package sqlite

import (
	"database/sql"
	"testing"
	"testing/fstest"
)

// Exercise a historical, populated upgrade without depending on the schema
// supported by the current repository. Version 2 exists only in this fixture.
func TestAccountMigrationPopulatedUpgrade(t *testing.T) {
	files := map[string]string{
		"000001_accounts.up.sql": accountBaselineSQL(t),
		"000002_display_email.up.sql": `ALTER TABLE user ADD COLUMN display_email TEXT NOT NULL DEFAULT '';
			UPDATE user SET display_email = email;
			CREATE INDEX user_display_email ON user(display_email);`,
		"000002_display_email.down.sql": `DROP INDEX user_display_email;
			ALTER TABLE user DROP COLUMN display_email;`,
	}
	directory := writeMigrationFiles(t, files)
	upgraded := unpreparedUserTestDB(t)
	run := migrationCommand(t, upgraded, directory, accountVersionTable)
	runMigration(t, run, "up", "1")
	if _, err := upgraded.ExecContext(t.Context(), accountBaselineRows); err != nil {
		t.Fatal(err)
	}
	beforeSchema := accountMigrationResultSnapshot(t, upgraded)
	beforeData := accountBaselineDataSnapshot(t, upgraded)
	runMigration(t, run, "up")
	if after := accountBaselineDataSnapshot(t, upgraded); after != beforeData {
		t.Fatal("upgrade changed existing accounts, identities, or tokens")
	}
	var displayEmail string
	if err := upgraded.QueryRowContext(t.Context(), `SELECT display_email FROM user WHERE id = 'account'`).Scan(&displayEmail); err != nil || displayEmail != "person@example.invalid" {
		t.Fatalf("backfilled email = %q, error = %v", displayEmail, err)
	}
	upgradedSchema := accountMigrationResultSnapshot(t, upgraded)

	// Both fresh entry points must produce the same final schema as an upgrade.
	for _, method := range []string{"bootstrap", "CLI"} {
		t.Run(method, func(t *testing.T) {
			fresh := unpreparedUserTestDB(t)
			if method == "CLI" {
				runMigration(t, migrationCommand(t, fresh, directory, accountVersionTable), "up")
			} else {
				source := fstest.MapFS{}
				for name, script := range files {
					source["migrations/"+name] = &fstest.MapFile{Data: []byte(script)}
				}
				tx, err := fresh.BeginTx(t.Context(), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if err := bootstrapAccountSchema(t.Context(), tx, source, 2); err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			if got := accountMigrationResultSnapshot(t, fresh); got != upgradedSchema {
				t.Fatalf("fresh schema differs from upgraded schema:\n%s\nwant:\n%s", got, upgradedSchema)
			}
		})
	}

	runMigration(t, run, "down", "1")
	if got := accountMigrationResultSnapshot(t, upgraded); got != beforeSchema {
		t.Fatalf("downgrade did not restore version 1 schema:\n%s\nwant:\n%s", got, beforeSchema)
	}
	if got := accountBaselineDataSnapshot(t, upgraded); got != beforeData {
		t.Fatal("downgrade changed existing accounts, identities, or tokens")
	}
	runMigration(t, run, "up")
	if got := accountMigrationResultSnapshot(t, upgraded); got != upgradedSchema {
		t.Fatal("repeated upgrade changed schema or version metadata")
	}
	if got := accountBaselineDataSnapshot(t, upgraded); got != beforeData {
		t.Fatal("repeated upgrade changed existing data")
	}
	if err := upgraded.QueryRowContext(t.Context(), `SELECT display_email FROM user WHERE id = 'account'`).Scan(&displayEmail); err != nil || displayEmail != "person@example.invalid" {
		t.Fatalf("repeated backfill = %q, error = %v", displayEmail, err)
	}
}

func accountMigrationResultSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	// The CLI adds an index to its version table. Compare all application
	// objects exactly, but compare bookkeeping by its recorded version and state.
	return accountStorageSnapshot(t, db,
		`SELECT type, name, tbl_name, sql FROM sqlite_master WHERE tbl_name != 'hyperserver_account_version' ORDER BY name`,
		`SELECT version, dirty FROM hyperserver_account_version ORDER BY version`,
	)
}

func accountBaselineDataSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	return accountStorageSnapshot(t, db,
		`SELECT id, email, email_key, verified, verification_required, created_at, updated_at, registration_auth_type, password, session_version FROM user ORDER BY id`,
		`SELECT issuer, subject, user_id FROM external_identity ORDER BY issuer, subject`,
		`SELECT user_id, token_hash, expires_at, token_type, email FROM usertoken ORDER BY user_id, token_hash`,
	)
}
