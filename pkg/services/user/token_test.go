package user

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/internal/opaquetoken"
)

func TestTokenPurposeValues(t *testing.T) {
	if ResetTokenPurpose != "auth-reset" || VerificationTokenPurpose != "auth-verification" {
		t.Fatal("token purposes must match stored metadata")
	}
}

func TestTokenCreationRejectsUnsupportedPurpose(t *testing.T) {
	for _, purpose := range []string{"", "session"} {
		t.Run(purpose, func(t *testing.T) {
			if _, err := newAuthToken(&User{ID: "test-user"}, time.Hour, purpose); err == nil {
				t.Fatal("created a token with an unsupported purpose")
			}
		})
	}
}

func TestOpaqueTokenFormat(t *testing.T) {
	calls := 0
	failure := errors.New("token lookup reached")
	repo := tokenLookupRepository{token: func(context.Context, string, string) (TokenMetadata, error) {
		calls++
		return TokenMetadata{}, failure
	}}
	for _, raw := range []string{"", "legacy.jwt.token", "hs1_invalid", opaquetoken.New() + "x", opaquetoken.Hash(opaquetoken.New())} {
		want := ErrToken
		if raw == "" {
			want = ErrTokenNotSpecified
		}
		if _, err := loadToken(t.Context(), repo, raw, ResetTokenPurpose); !errors.Is(err, want) || calls != 0 {
			t.Fatalf("malformed token: error=%v, want=%v, repository calls=%d", err, want, calls)
		}
	}
	if _, err := loadToken(t.Context(), repo, opaquetoken.New(), ResetTokenPurpose); !errors.Is(err, failure) || calls != 1 {
		t.Fatalf("valid format did not reach repository: error=%v, calls=%d", err, calls)
	}
	u := &User{ID: "account", Email: "person@example.invalid"}
	seen := map[string]bool{}
	for range 100 {
		token, err := newAuthToken(u, time.Hour, VerificationTokenPurpose)
		if err != nil || !opaquetoken.Valid(token.Token) || token.TokenHash != opaquetoken.Hash(token.Token) || seen[token.Token] {
			t.Fatal("invalid or duplicate token", err)
		}
		seen[token.Token] = true
		if token.Metadata().Email != u.Email {
			t.Fatal("recipient not bound")
		}
	}
}

type tokenLookupRepository struct {
	AccountRepository
	TokenRepository
	token   func(context.Context, string, string) (TokenMetadata, error)
	account func(context.Context, string) (*User, error)
}

func (s tokenLookupRepository) GetToken(ctx context.Context, hash, purpose string) (TokenMetadata, error) {
	return s.token(ctx, hash, purpose)
}

func (s tokenLookupRepository) GetByID(ctx context.Context, id string) (*User, error) {
	return s.account(ctx, id)
}

func TestValidateTokenUsesRepository(t *testing.T) {
	for _, purpose := range []string{ResetTokenPurpose, VerificationTokenPurpose} {
		for _, scenario := range []string{"success", "malformed", "wrong purpose", "empty token", "nil repository", "missing token", "token failure", "wrong stored account", "wrong stored hash", "wrong stored purpose", "stored expiry", "missing account", "account failure", "nil account", "wrong account", "changed email", "canceled", "account canceled"} {
			t.Run(purpose+"/"+scenario, func(t *testing.T) {
				account := &User{ID: "account", Email: "person@example.invalid"}
				tokenPurpose := purpose
				if scenario == "wrong purpose" {
					tokenPurpose = ResetTokenPurpose
					if purpose == ResetTokenPurpose {
						tokenPurpose = VerificationTokenPurpose
					}
				}
				token, err := newAuthToken(account, time.Hour, tokenPurpose)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "malformed" {
					token.Token = "not-a-token"
				} else if scenario == "empty token" {
					token.Token = ""
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if scenario == "canceled" {
					cancel()
				}
				failure := errors.New("storage unavailable")
				tokenCalls, accountCalls := 0, 0
				var repository AccountRepository = tokenLookupRepository{
					token: func(got context.Context, hash, requestedPurpose string) (TokenMetadata, error) {
						tokenCalls++
						if got != ctx || hash != token.TokenHash || requestedPurpose != purpose {
							t.Fatal("token lookup lost its context, hash, or purpose")
						}
						metadata := token.Metadata()
						switch scenario {
						case "missing token":
							return TokenMetadata{}, ErrTokenNotFound
						case "token failure":
							return TokenMetadata{}, failure
						case "wrong stored account":
							metadata.AccountID = ""
						case "wrong stored hash":
							metadata.TokenHash = "another-hash"
						case "wrong stored purpose":
							metadata.Purpose = "another-purpose"
						case "stored expiry":
							metadata.ExpiresAt = time.Now().Add(-time.Hour)
						case "canceled":
							return TokenMetadata{}, got.Err()
						}
						return metadata, nil
					},
					account: func(got context.Context, id string) (*User, error) {
						accountCalls++
						if got != ctx || id != token.UserId {
							t.Fatal("account lookup lost its context or ID")
						}
						switch scenario {
						case "missing account":
							return nil, ErrUserNotFound
						case "account failure":
							return nil, failure
						case "account canceled":
							cancel()
							return nil, got.Err()
						case "nil account":
							return nil, nil
						case "wrong account":
							return &User{ID: "another-account"}, nil
						case "changed email":
							account.Email = "new@example.invalid"
						}
						return account, nil
					},
				}
				if scenario == "nil repository" {
					repository = nil
				}
				validate := ValidateResetToken
				if purpose == VerificationTokenPurpose {
					validate = ValidateVerificationToken
				}
				got, err := validate(ctx, repository, token.Token)
				wantSuccess := scenario == "success" || (scenario == "changed email" && purpose == ResetTokenPurpose)
				if wantSuccess {
					if err != nil || got != account {
						t.Fatalf("valid token rejected: %v, %v", got, err)
					}
				} else if got != nil || err == nil {
					t.Fatalf("invalid token accepted: %v, %v", got, err)
				}
				wantTokenCalls, wantAccountCalls := 1, 0
				switch scenario {
				case "malformed", "empty token", "nil repository":
					wantTokenCalls = 0
				case "success", "missing account", "account failure", "nil account", "wrong account", "changed email", "account canceled":
					wantAccountCalls = 1
				}
				if tokenCalls != wantTokenCalls || accountCalls != wantAccountCalls {
					t.Fatalf("token calls=%d, account calls=%d, want %d and %d", tokenCalls, accountCalls, wantTokenCalls, wantAccountCalls)
				}
				if (scenario == "token failure" || scenario == "account failure") && !errors.Is(err, failure) {
					t.Fatalf("storage failure was hidden: %v", err)
				}
				if (scenario == "canceled" || scenario == "account canceled") && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation was hidden: %v", err)
				}
				if (scenario == "nil account" || scenario == "wrong account") && IsTokenRejection(err) {
					t.Fatalf("repository contract violation became a token rejection: %v", err)
				}
			})
		}
	}
}

func TestValidateTokenExpiryDuringLookup(t *testing.T) {
	for _, purpose := range []string{ResetTokenPurpose, VerificationTokenPurpose} {
		for _, waitAt := range []string{"token", "account"} {
			t.Run(purpose+"/"+waitAt, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					account := &User{ID: "account", Email: "person@example.invalid"}
					token, err := newAuthToken(account, time.Second, purpose)
					if err != nil {
						t.Fatal(err)
					}
					repository := tokenLookupRepository{
						token: func(context.Context, string, string) (TokenMetadata, error) {
							if waitAt == "token" {
								time.Sleep(time.Second)
							}
							return token.Metadata(), nil
						},
						account: func(context.Context, string) (*User, error) {
							if waitAt == "account" {
								time.Sleep(time.Second)
							}
							return account, nil
						},
					}
					got, err := validateToken(context.Background(), repository, token.Token, purpose)
					if got != nil || !errors.Is(err, ErrTokenExpired) {
						t.Fatalf("lookup accepted token at its expiry boundary: %v, %v", got, err)
					}
				})
			})
		}
	}
}
