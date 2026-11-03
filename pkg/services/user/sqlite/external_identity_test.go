package sqlite

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHasExternalIdentity(t *testing.T) {
	repo := NewAccountRepository(userTestDB(t))
	account := &user.User{Email: "person@example.com", Verified: true}
	id := user.ExternalIdentity{Issuer: "https://issuer.example", Subject: "subject"}
	if err := repo.CreateExternalAccount(t.Context(), account, id); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		account, issuer string
		want            bool
	}{
		{account.ID, id.Issuer, true},
		{account.ID, "https://other.example", false},
		{"missing", id.Issuer, false},
	} {
		got, err := repo.HasExternalIdentity(t.Context(), tc.account, tc.issuer)
		if err != nil || got != tc.want {
			t.Fatalf("lookup (%q, %q): %v, %v", tc.account, tc.issuer, got, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repo.HasExternalIdentity(ctx, account.ID, id.Issuer); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lookup: %v", err)
	}
	if _, err := NewAccountRepository(nil).HasExternalIdentity(t.Context(), account.ID, id.Issuer); !errors.Is(err, database.ErrDatabaseUnavailable) {
		t.Fatalf("unavailable lookup: %v", err)
	}
}

func TestExternalIdentityWritesAreAtomic(t *testing.T) {
	db := userTestDB(t)
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	repo := NewAccountRepository(db)
	id := user.ExternalIdentity{Issuer: "https://issuer.example", Subject: "one"}
	first := &user.User{Email: "first@example.com", Verified: true}
	if err := repo.CreateExternalAccount(t.Context(), first, id); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetExternalAccount(t.Context(), id); err != nil || got.ID != first.ID {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	duplicate := &user.User{Email: "second@example.com"}
	if err := repo.CreateExternalAccount(t.Context(), duplicate, id); !errors.Is(err, user.ErrIdentityConflict) {
		t.Fatal("duplicate identity", err)
	}
	if duplicate.ID != "" {
		t.Fatal("failed create mutated input")
	}
	if _, err := repo.GetByEmail(t.Context(), duplicate.Email); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatal("orphan account", err)
	}
	duplicate.Email = first.Email
	if err := repo.CreateExternalAccount(t.Context(), duplicate, user.ExternalIdentity{Issuer: id.Issuer, Subject: "two"}); !errors.Is(err, user.ErrEmailAlreadyExists) {
		t.Fatal("implicit email linking", err)
	}
	before := *first
	if err := repo.LinkExternalIdentity(t.Context(), first, id); !errors.Is(err, user.ErrIdentityConflict) {
		t.Fatal("repeated linking", err)
	}
	got, err := repo.GetByID(t.Context(), first.ID)
	if err != nil || *first != before || got.SessionVersion != before.SessionVersion {
		t.Fatal("failed link changed credentials", err)
	}
	if _, err := db.Exec(`DELETE FROM user WHERE id = ?`, first.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM external_identity`).Scan(&count); err != nil || count != 0 {
		t.Fatal("identity not deleted with account", count, err)
	}
}

func TestExternalIdentityConcurrentLink(t *testing.T) {
	repo := NewAccountRepository(userTestDB(t))
	account := &user.User{Email: "person@example.com", Password: "preserved", RegistrationAuthType: "email"}
	if err := repo.Create(t.Context(), account); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	var successes atomic.Int32
	for range 4 {
		group.Go(func() {
			copy := *account
			if err := repo.LinkExternalIdentity(t.Context(), &copy, user.ExternalIdentity{Issuer: "https://issuer.example", Subject: "subject"}); err == nil {
				successes.Add(1)
			}
		})
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatal("link successes", successes.Load())
	}
	got, err := repo.GetByID(t.Context(), account.ID)
	if err != nil || got.Password != account.Password || got.SessionVersion != account.SessionVersion+1 {
		t.Fatal("account changed unexpectedly", got, err)
	}
}

func TestExternalIdentityMigrationUpDown(t *testing.T) {
	db := unpreparedUserTestDB(t)
	if _, err := db.Exec(accountVersionSchema + `; INSERT INTO hyperserver_account_version VALUES (1, false);` + accountSchemaV1); err != nil {
		t.Fatal(err)
	}
	repo := NewAccountRepository(db)
	u := &user.User{Email: "preserved@example.com", Password: "password"}
	if err := repo.Create(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(accountSchemaV2 + `UPDATE hyperserver_account_version SET version = 2`); err != nil {
		t.Fatal(err)
	}
	assertAccountSetupRejected(t, db, "requires version 3")
	if err := repo.LinkExternalIdentity(t.Context(), u, user.ExternalIdentity{Issuer: "issuer", Subject: "subject"}); err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("migrations/000002_external_identities.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(down) + `UPDATE hyperserver_account_version SET version = 1`); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(t.Context(), u.ID)
	if err != nil || got.Password != "password" {
		t.Fatal("down removed local account", got, err)
	}
	if _, err := db.Exec(accountSchemaV2 + `UPDATE hyperserver_account_version SET version = 2`); err != nil {
		t.Fatal(err)
	}
	assertAccountSetupRejected(t, db, "requires version 3")
	if _, err := repo.GetExternalAccount(t.Context(), user.ExternalIdentity{Issuer: "issuer", Subject: "subject"}); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatal("down retained identity", err)
	}
}
