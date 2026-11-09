package user

import (
	"context"
	"errors"
	"testing"
	"time"
)

type verificationAccountRepository struct {
	AccountRepository
	TokenRepository
	verify func(context.Context, VerificationAuthorization) (*User, error)
}

func (s verificationAccountRepository) GetToken(ctx context.Context, hash, purpose string) (TokenMetadata, error) {
	return s.AccountRepository.(TokenRepository).GetToken(ctx, hash, purpose)
}

func (s verificationAccountRepository) Verify(ctx context.Context, authorization VerificationAuthorization) (*User, error) {
	return s.verify(ctx, authorization)
}

func TestVerifyValidatesBeforeCallingRepository(t *testing.T) {
	for _, scenario := range []string{"success", "storage failure", "canceled", "wrong hash", "wrong purpose", "missing account", "missing email", "expired", "malformed", "nil repository"} {
		t.Run(scenario, func(t *testing.T) {
			account := &User{ID: "account", Email: "person@example.invalid", Verified: true, SessionVersion: 2}
			token, err := NewAuthVerificationToken(account, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			raw := token.Token
			switch scenario {
			case "wrong purpose":
				token.Type = ResetTokenPurpose
			case "missing account":
				token.UserId = ""
			case "missing email":
				token.Email = ""
			case "expired":
				token.ExpiresAt = time.Now().Add(-time.Hour)
			case "wrong hash":
				token.TokenHash = "other-hash"
			case "malformed":
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
			var repository AccountRepository = verificationAccountRepository{AccountRepository: tokenLookupRepository{token: func(context.Context, string, string) (TokenMetadata, error) { return token.Metadata(), nil }}, verify: func(got context.Context, authorization VerificationAuthorization) (*User, error) {
				calls++
				if got != ctx || authorization.AccountID != account.ID || authorization.Email != account.Email || authorization.TokenHash != token.TokenHash || !authorization.ExpiresAt.Equal(token.ExpiresAt) {
					t.Fatal("verification lost its context or stored authorization")
				}
				if wantErr != nil {
					return nil, wantErr
				}
				return account, nil
			}}
			if scenario == "nil repository" {
				repository = nil
			}
			got, err := Verify(ctx, repository, raw)
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
