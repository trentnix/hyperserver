package user

import (
	"context"
	"errors"
	"testing"
	"time"
)

type resetAccountRepository struct {
	AccountRepository
	TokenRepository
	reset func(context.Context, *User, ResetAuthorization, string) error
}

func (s resetAccountRepository) GetToken(ctx context.Context, hash, purpose string) (TokenMetadata, error) {
	return s.AccountRepository.(TokenRepository).GetToken(ctx, hash, purpose)
}

func (s resetAccountRepository) ResetPassword(ctx context.Context, u *User, authorization ResetAuthorization, hash string) error {
	return s.reset(ctx, u, authorization, hash)
}

func TestResetPasswordValidatesBeforeCallingRepository(t *testing.T) {
	for _, scenario := range []string{"success", "storage failure", "canceled", "wrong hash", "wrong purpose", "wrong account", "expired", "malformed", "empty hash", "nil account", "missing ID", "nil repository"} {
		t.Run(scenario, func(t *testing.T) {
			u := &User{ID: "account", Email: "person@example.invalid", Password: "old hash", SessionVersion: 1}
			purpose, lifetime := ResetTokenPurpose, time.Hour
			if scenario == "wrong purpose" {
				purpose = VerificationTokenPurpose
			}
			token, err := newAuthToken(u, lifetime, purpose)
			if err != nil {
				t.Fatal(err)
			}
			hash := "new hash"
			switch scenario {
			case "wrong hash":
				token.TokenHash = "other-hash"
			case "wrong account":
				u.ID = "other-account"
			case "malformed":
				token.Token = "not-a-token"
			case "expired":
				token.ExpiresAt = time.Now().Add(-time.Hour)
			case "empty hash":
				hash = ""
			case "nil account":
				u = nil
			case "missing ID":
				u.ID = ""
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
			var repository AccountRepository = resetAccountRepository{AccountRepository: tokenLookupRepository{token: func(context.Context, string, string) (TokenMetadata, error) { return token.Metadata(), nil }}, reset: func(got context.Context, account *User, authorization ResetAuthorization, passwordHash string) error {
				calls++
				if got != ctx || account != u || passwordHash != hash || authorization.AccountID != token.UserId || authorization.TokenHash != token.TokenHash || !authorization.ExpiresAt.Equal(token.ExpiresAt) {
					t.Fatal("reset lost its context, account, password hash, or stored authorization")
				}
				return wantErr
			}}
			if scenario == "nil repository" {
				repository = nil
			}
			err = u.ResetPassword(ctx, repository, token.Token, hash)
			if scenario == "success" || wantErr != nil {
				if calls != 1 || !errors.Is(err, wantErr) {
					t.Fatalf("reset error=%v, repository calls=%d, want %v and 1", err, calls, wantErr)
				}
			} else if err == nil || calls != 0 {
				t.Fatalf("invalid reset reached storage: error=%v, calls=%d", err, calls)
			}
		})
	}
}
