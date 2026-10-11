package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type verificationAccountRepository struct {
	AccountRepository
	verify func(context.Context, VerificationAuthorization) (*User, error)
}

func (s verificationAccountRepository) Verify(ctx context.Context, authorization VerificationAuthorization) (*User, error) {
	return s.verify(ctx, authorization)
}

func TestVerifyValidatesBeforeCallingRepository(t *testing.T) {
	for _, scenario := range []string{"success", "storage failure", "canceled", "wrong signature", "wrong purpose", "missing account", "missing email", "expired", "malformed", "nil repository"} {
		t.Run(scenario, func(t *testing.T) {
			account := &User{ID: "account", Email: "person@example.invalid", Verified: true, SessionVersion: 2}
			key := []byte("test-signing-key")
			claims := &VerificationClaims{Id: account.ID, Email: account.Email, Purpose: VerificationTokenPurpose,
				RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
			switch scenario {
			case "wrong purpose":
				claims.Purpose = ResetTokenPurpose
			case "missing account":
				claims.Id = ""
			case "missing email":
				claims.Email = ""
			case "expired":
				claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
			}
			raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "wrong signature" {
				key = []byte("wrong-key")
			} else if scenario == "malformed" {
				raw = "not-a-token"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wantErr error
			if scenario == "storage failure" {
				wantErr = errors.New("storage unavailable")
			} else if scenario == "canceled" {
				cancel()
				wantErr = context.Canceled
			}
			calls := 0
			hash := sha256.Sum256([]byte(raw))
			var repository AccountRepository = verificationAccountRepository{verify: func(got context.Context, authorization VerificationAuthorization) (*User, error) {
				calls++
				if got != ctx || authorization.AccountID != account.ID || authorization.Email != account.Email || authorization.TokenHash != hex.EncodeToString(hash[:]) || !authorization.ExpiresAt.Equal(claims.ExpiresAt.Time) {
					t.Fatal("verification lost its context or signed authorization")
				}
				if wantErr != nil {
					return nil, wantErr
				}
				return account, nil
			}}
			if scenario == "nil repository" {
				repository = nil
			}
			got, err := Verify(ctx, repository, raw, key)
			if scenario == "success" {
				if err != nil || calls != 1 || got != account {
					t.Fatalf("verification result=%v, error=%v, calls=%d", got, err, calls)
				}
			} else if wantErr != nil {
				if got != nil || calls != 1 || !errors.Is(err, wantErr) {
					t.Fatalf("verification lost repository failure: result=%v, error=%v, calls=%d", got, err, calls)
				}
			} else if got != nil || err == nil || calls != 0 {
				t.Fatalf("invalid verification reached storage: result=%v, error=%v, calls=%d", got, err, calls)
			}
		})
	}
}
