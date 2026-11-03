package sqlite

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestOpaqueTokenMigrationUpDown(t *testing.T) {
	db := unpreparedUserTestDB(t)
	if _, err := db.Exec(accountVersionSchema + `; INSERT INTO hyperserver_account_version VALUES (2,false);` + accountSchemaV1 + accountSchemaV2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO user (id,email,email_key,password,registration_auth_type) VALUES ('account','person@example.invalid','PERSON@EXAMPLE.INVALID','password','email');
	INSERT INTO external_identity (issuer,subject,user_id) VALUES ('issuer','subject','account');
	INSERT INTO usertoken (user_id,token_hash,expires_at,token_type) VALUES ('account','old-hash','2030-01-01','auth-verification')`); err != nil {
		t.Fatal(err)
	}
	assertAccountSetupRejected(t, db, "requires version 3")
	if _, err := db.Exec(accountSchemaV3 + `UPDATE hyperserver_account_version SET version=3`); err != nil {
		t.Fatal(err)
	}
	assertTokenLookupIndex(t, db)
	repo := NewAccountRepository(db)
	if err := repo.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := repo.GetExternalAccount(t.Context(), user.ExternalIdentity{Issuer: "issuer", Subject: "subject"})
	if err != nil || u.Password != "password" {
		t.Fatal("migration changed account or identity", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM usertoken`).Scan(&count); err != nil || count != 0 {
		t.Fatal("old links preserved", err)
	}
	token, err := user.NewAuthVerificationToken(u, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertToken(t.Context(), token.Metadata()); err != nil {
		t.Fatal(err)
	}
	if got, err := user.Verify(t.Context(), repo, token.Token); err != nil || !got.Verified {
		t.Fatal("new link rejected", err)
	}
	if err := repo.InsertToken(t.Context(), token.Metadata()); err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000003_opaque_tokens.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(down) + `UPDATE hyperserver_account_version SET version=2`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='index' AND name='usertoken_lookup'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("lookup index survived downgrade", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM usertoken`).Scan(&count); err != nil || count != 0 {
		t.Fatal("opaque links survived downgrade", err)
	}
	if got, err := repo.GetExternalAccount(t.Context(), user.ExternalIdentity{Issuer: "issuer", Subject: "subject"}); err != nil || got.Password != "password" || !got.Verified {
		t.Fatal("downgrade changed account or identity", err)
	}
	if _, err := db.Exec(accountSchemaV3 + `UPDATE hyperserver_account_version SET version=3`); err != nil {
		t.Fatal(err)
	}
	if err := repo.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertTokenLookupIndex(t, db)
}

func TestFreshAccountTokenLookupIndex(t *testing.T) {
	assertTokenLookupIndex(t, userTestDB(t))
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
