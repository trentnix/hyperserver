package sqlite

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/internal/accounttest"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestSQLiteAccountContract(t *testing.T) {
	accounttest.Run(t, func(t *testing.T) user.AccountRepository {
		db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "accounts.db")+"?_journal=WAL&_timeout=5000&_fk=true")
		if err != nil {
			t.Fatal(err)
		}
		// Use two connections so competing writes also exercise SQLite's locking.
		db.SetMaxOpenConns(2)
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
		repo := NewAccountRepository(db)
		if err := repo.Initialize(t.Context()); err != nil {
			t.Fatal(err)
		}
		return repo
	})
}

func TestSQLiteAccountCreationTime(t *testing.T) {
	repo := NewAccountRepository(userTestDB(t))
	u := &user.User{Email: "created@example.invalid", CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0)}
	started := time.Now()
	if err := repo.Create(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	if u.CreatedAt.Before(started) || u.CreatedAt.After(time.Now()) || !u.CreatedAt.Equal(u.UpdatedAt) {
		t.Fatalf("incorrect creation timestamps: %+v", u)
	}
	stored, err := repo.GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertAccountFields(t, *stored, *u)
}

// This provider checks deadlines with Go's clock. A remote backend must test
// exact expiry against its own clock rather than the client's virtual clock.
func TestSQLiteTokenExpiryBoundary(t *testing.T) {
	for _, purpose := range []string{user.ResetTokenPurpose, user.VerificationTokenPurpose} {
		for _, expiry := range []string{"stored", "signed"} {
			t.Run(purpose+"/"+expiry, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					repo := NewAccountRepository(userTestDB(t))
					u := &user.User{Email: "expiry@example.invalid", Password: "hash", RegistrationAuthType: "email"}
					if err := repo.Create(t.Context(), u); err != nil {
						t.Fatal(err)
					}
					before := *u
					token := user.TokenMetadata{AccountID: u.ID, TokenHash: "expiry-boundary", Purpose: purpose, ExpiresAt: time.Now().Add(time.Hour)}
					if err := repo.InsertToken(t.Context(), token); err != nil {
						t.Fatal(err)
					}
					deadline, signedExpiry := token.ExpiresAt, token.ExpiresAt.Add(time.Hour)
					if expiry == "signed" {
						deadline = token.ExpiresAt.Add(-time.Minute)
						signedExpiry = deadline
					}

					time.Sleep(time.Until(deadline))
					var result *user.User
					var err error
					if purpose == user.ResetTokenPurpose {
						err = repo.ResetPassword(t.Context(), u, user.ResetAuthorization{
							AccountID: u.ID, TokenHash: token.TokenHash, ExpiresAt: signedExpiry,
						}, "new hash")
					} else {
						result, err = repo.Verify(t.Context(), user.VerificationAuthorization{
							AccountID: u.ID, Email: u.Email, TokenHash: token.TokenHash, ExpiresAt: signedExpiry,
						})
					}
					var expired *user.ErrTokenExpired
					if result != nil || !errors.As(err, &expired) {
						t.Fatalf("redemption at expiry = %v, %v", result, err)
					}
					assertAccountFields(t, *u, before)
					stored, err := repo.GetByID(t.Context(), u.ID)
					if err != nil {
						t.Fatal(err)
					}
					assertAccountFields(t, *stored, before)
					metadata, err := repo.GetToken(t.Context(), token.TokenHash, purpose)
					if err != nil || metadata.AccountID != token.AccountID || metadata.TokenHash != token.TokenHash || metadata.Purpose != purpose || !metadata.ExpiresAt.Equal(token.ExpiresAt) {
						t.Fatalf("expiry changed stored token: %+v, %v", metadata, err)
					}
				})
			})
		}
	}
}
