package sqlite

import (
	"reflect"
	"testing"
	"time"

	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAccountFieldMappings(t *testing.T) {
	for _, operation := range []string{"lookup ID", "lookup email", "update", "reset", "verify"} {
		t.Run(operation, func(t *testing.T) {
			db := userTestDB(t)
			repository := NewAccountRepository(db)
			original := accounts.User{
				ID: "mapping-account", Email: "reader@example.invalid", Password: "original hash",
				Verified: false, VerificationRequired: true, RegistrationAuthType: "email", SessionVersion: 7,
				CreatedAt: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
				UpdatedAt: time.Date(2021, 6, 7, 8, 9, 10, 0, time.UTC),
			}
			// Seed independently of the repository's write and scan mappings.
			_, err := db.ExecContext(t.Context(), `INSERT INTO user
				(id, email, email_key, password, registration_auth_type, verified, verification_required, session_version, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, original.ID, original.Email, accounts.EmailKey(original.Email), original.Password,
				original.RegistrationAuthType, original.Verified, original.VerificationRequired,
				original.SessionVersion, original.CreatedAt, original.UpdatedAt)
			if err != nil {
				t.Fatal(err)
			}
			want, changed := original, original
			var got *accounts.User
			started := time.Now()
			switch operation {
			case "lookup ID":
				got, err = repository.GetByID(t.Context(), original.ID)
			case "lookup email":
				got, err = repository.GetByEmail(t.Context(), original.Email)
			case "update":
				changed.Email, changed.Password = "o'neil@example.invalid", "replacement hash"
				changed.RegistrationAuthType = "another provider"
				changed.Verified, changed.VerificationRequired = true, false
				want = changed
				want.SessionVersion++
				err = repository.Update(t.Context(), &changed)
				got = &changed
			case "reset", "verify":
				purpose := accounts.ResetTokenPurpose
				if operation == "verify" {
					purpose = accounts.VerificationTokenPurpose
				}
				expires := time.Now().Add(time.Hour)
				if err := repository.InsertToken(t.Context(), accounts.TokenMetadata{
					AccountID: original.ID, TokenHash: "stored hash", Purpose: purpose, ExpiresAt: expires,
				}); err != nil {
					t.Fatal(err)
				}
				want.SessionVersion++
				if operation == "reset" {
					want.Password = "new hash"
					err = repository.ResetPassword(t.Context(), &changed, accounts.ResetAuthorization{
						AccountID: original.ID, TokenHash: "stored hash", ExpiresAt: expires,
					}, want.Password)
					got = &changed
				} else {
					want.Verified = true
					got, err = repository.Verify(t.Context(), accounts.VerificationAuthorization{
						AccountID: original.ID, Email: original.Email, TokenHash: "stored hash", ExpiresAt: expires,
					})
				}
			}
			if err != nil || got == nil {
				t.Fatalf("operation returned %v, %v", got, err)
			}
			if operation == "update" || operation == "reset" || operation == "verify" {
				if got.UpdatedAt.Before(started) || got.UpdatedAt.After(time.Now()) {
					t.Fatalf("incorrect update time: %v", got.UpdatedAt)
				}
				want.UpdatedAt = got.UpdatedAt
			}
			assertAccountFields(t, *got, want)

			var stored accounts.User
			err = db.QueryRowContext(t.Context(), `SELECT password, email, id, registration_auth_type,
				verification_required, verified, session_version, updated_at, created_at FROM user WHERE id = ?`, original.ID).
				Scan(&stored.Password, &stored.Email, &stored.ID, &stored.RegistrationAuthType,
					&stored.VerificationRequired, &stored.Verified, &stored.SessionVersion, &stored.UpdatedAt, &stored.CreatedAt)
			if err != nil {
				t.Fatal(err)
			}
			assertAccountFields(t, stored, want)
		})
	}
}

func assertAccountFields(t *testing.T, got, want accounts.User) {
	t.Helper()
	got.CreatedAt, want.CreatedAt = got.CreatedAt.UTC(), want.CreatedAt.UTC()
	got.UpdatedAt, want.UpdatedAt = got.UpdatedAt.UTC(), want.UpdatedAt.UTC()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("account fields = %+v, want %+v", got, want)
	}
}

func TestAccountScanFailure(t *testing.T) {
	for _, operation := range []string{"lookup ID", "lookup email", "reset", "verify"} {
		t.Run(operation, func(t *testing.T) {
			db, account, _, _ := verificationFixture(t)
			repository := NewAccountRepository(db)
			before := *account
			purpose := accounts.ResetTokenPurpose
			if operation == "verify" {
				purpose = accounts.VerificationTokenPurpose
			}
			expires := time.Now().Add(time.Hour)
			if err := repository.InsertToken(t.Context(), accounts.TokenMetadata{
				AccountID: account.ID, TokenHash: "scan-failure", Purpose: purpose, ExpiresAt: expires,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), `UPDATE user SET verification_required = 'invalid' WHERE id = ?`, account.ID); err != nil {
				t.Fatal(err)
			}

			var got *accounts.User
			var err error
			switch operation {
			case "lookup ID":
				got, err = repository.GetByID(t.Context(), account.ID)
			case "lookup email":
				got, err = repository.GetByEmail(t.Context(), account.Email)
			case "reset":
				err = repository.ResetPassword(t.Context(), account, accounts.ResetAuthorization{
					AccountID: account.ID, TokenHash: "scan-failure", ExpiresAt: expires,
				}, "new hash")
			case "verify":
				got, err = repository.Verify(t.Context(), accounts.VerificationAuthorization{
					AccountID: account.ID, Email: account.Email, TokenHash: "scan-failure", ExpiresAt: expires,
				})
			}
			if err == nil || got != nil || *account != before {
				t.Fatalf("scan failure returned an account or changed the caller: %v, %v", got, err)
			}
			if _, err := repository.GetToken(t.Context(), "scan-failure", purpose); err != nil {
				t.Fatal("scan failure consumed the token:", err)
			}
			stored := before
			var required string
			err = db.QueryRowContext(t.Context(), `SELECT password, verified, session_version, updated_at, verification_required FROM user WHERE id = ?`, account.ID).
				Scan(&stored.Password, &stored.Verified, &stored.SessionVersion, &stored.UpdatedAt, &required)
			if err != nil || required != "invalid" {
				t.Fatalf("read after scan failure: %v, verification_required=%q", err, required)
			}
			assertAccountFields(t, stored, before)
		})
	}
}
