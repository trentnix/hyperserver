package sqlite

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Exercise the baseline SQL even when the optional migration CLI is unavailable.
func TestAccountBaselineUpDown(t *testing.T) {
	db := unpreparedUserTestDB(t)
	if _, err := db.Exec(accountVersionSchema + `; INSERT INTO hyperserver_account_version VALUES (1, false);` + accountBaselineSQL(t)); err != nil {
		t.Fatal(err)
	}
	before := accountSchemaSnapshot(t, db)
	// Historical SQL must remain testable without the current repository.
	if _, err := db.ExecContext(t.Context(), accountBaselineRows); err != nil {
		t.Fatal(err)
	}
	assertTokenLookupIndex(t, db)
	// Leave dependent data in place to exercise foreign-key-safe drop ordering.
	down, err := os.ReadFile("migrations/000001_accounts.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(down) + `DELETE FROM hyperserver_account_version`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('user', 'usertoken', 'external_identity', 'usertoken_expiration', 'usertoken_lookup')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("baseline down left account objects", count, err)
	}
	if _, err := db.Exec(accountBaselineSQL(t) + `INSERT INTO hyperserver_account_version VALUES (1, false)`); err != nil {
		t.Fatal(err)
	}
	if after := accountSchemaSnapshot(t, db); after != before {
		t.Fatal("up/down/up changed the baseline schema")
	}
	assertTokenLookupIndex(t, db)
}

// These rows describe version 1, independent of current application models.
const accountBaselineRows = `
INSERT INTO user (id, email, email_key, password, created_at, updated_at)
VALUES ('account', 'person@example.invalid', 'person@example.invalid', 'password', '2030-01-01', '2030-01-01');
INSERT INTO external_identity (issuer, subject, user_id) VALUES ('issuer', 'subject', 'account');
INSERT INTO usertoken (user_id, token_hash, expires_at, token_type, email)
VALUES ('account', 'hash', '2030-02-01', 'verification', 'person@example.invalid');`

func TestCurrentAccountSchemaTokensAndIdentity(t *testing.T) {
	db := userTestDB(t) // Initializes the complete embedded migration chain.
	repo := NewAccountRepository(db)
	u := &user.User{Email: "person@example.invalid", Password: "password"}
	identity := user.ExternalIdentity{Issuer: "issuer", Subject: "subject"}
	if err := repo.CreateExternalAccount(t.Context(), u, identity); err != nil {
		t.Fatal(err)
	}
	token, err := user.NewAuthVerificationToken(u, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertToken(t.Context(), token.Metadata()); err != nil {
		t.Fatal(err)
	}
	if err := PrepareDatabase(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if got, err := user.Verify(t.Context(), repo, token.Token); err != nil || !got.Verified {
		t.Fatal("verification failed", err)
	}
	if got, err := repo.GetExternalAccount(t.Context(), identity); err != nil || got.Password != "password" {
		t.Fatal("identity lookup failed", err)
	}
}

func TestFreshAccountTokenLookupIndex(t *testing.T) {
	assertTokenLookupIndex(t, userTestDB(t))
}

func accountBaselineSQL(t *testing.T) string {
	t.Helper()
	script, err := accountMigrations.ReadFile("migrations/000001_accounts.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	return string(script)
}

func TestAccountBootstrapDiscoversMigrations(t *testing.T) {
	db := unpreparedUserTestDB(t)
	source := fstest.MapFS{
		"migrations/000001_initial.up.sql":   {Data: []byte("CREATE TABLE user (id TEXT);")},
		"migrations/000002_change.up.sql":    {Data: []byte("ALTER TABLE user ADD COLUMN email TEXT;")},
		"migrations/000001_initial.down.sql": {Data: []byte("DROP TABLE user;")},
	}
	tx, err := db.BeginTx(t.Context(), nil)
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
	if _, err := db.ExecContext(t.Context(), `INSERT INTO user (id, email) VALUES ('test', 'test@example.invalid')`); err != nil {
		t.Fatal("second migration was not applied:", err)
	}
	var version int
	var dirty bool
	if err := db.QueryRowContext(t.Context(), `SELECT version, dirty FROM hyperserver_account_version`).Scan(&version, &dirty); err != nil || version != 2 || dirty {
		t.Fatalf("version = %d, dirty = %t, error = %v", version, dirty, err)
	}
}

func TestAccountBootstrapMigrationFailuresRollBack(t *testing.T) {
	for _, tc := range []struct {
		name    string
		files   fstest.MapFS
		version int
	}{
		{"missing", fstest.MapFS{}, 1},
		{"extra", fstest.MapFS{
			"migrations/000001_initial.up.sql": {},
			"migrations/000002_extra.up.sql":   {},
		}, 1},
		{"duplicate", fstest.MapFS{
			"migrations/000001_initial.up.sql":   {Data: []byte("CREATE TABLE user (id TEXT);")},
			"migrations/000001_duplicate.up.sql": {},
		}, 2},
		{"gap", fstest.MapFS{
			"migrations/000001_initial.up.sql": {Data: []byte("CREATE TABLE user (id TEXT);")},
			"migrations/000003_gap.up.sql":     {},
		}, 2},
		{"invalid filename", fstest.MapFS{"migrations/invalid.up.sql": {}}, 1},
		{"later SQL failure", fstest.MapFS{
			"migrations/000001_initial.up.sql": {Data: []byte("CREATE TABLE user (id TEXT);")},
			"migrations/000002_failure.up.sql": {Data: []byte("INSERT INTO missing_table VALUES (1);")},
		}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := unpreparedUserTestDB(t)
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := bootstrapAccountSchema(t.Context(), tx, tc.files, tc.version); err == nil {
				t.Fatal("invalid migration set accepted")
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			assertNoAccountTables(t, db)
		})
	}
}

func assertTokenLookupIndex(t *testing.T, db *sql.DB) {
	t.Helper()
	var id, parent, unused int
	var detail string
	err := db.QueryRow(`EXPLAIN QUERY PLAN SELECT user_id, token_hash, expires_at, token_type, email FROM usertoken WHERE token_hash = ? AND token_type = ?`, "hash", user.ResetTokenPurpose).Scan(&id, &parent, &unused, &detail)
	if err != nil || !strings.Contains(detail, "USING INDEX usertoken_lookup (token_hash=? AND token_type=?)") {
		t.Fatalf("token lookup does not use its index: %q, %v", detail, err)
	}
}
