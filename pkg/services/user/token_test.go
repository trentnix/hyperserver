package user

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenPurposeValues(t *testing.T) {
	if ResetTokenPurpose != "auth-reset" || VerificationTokenPurpose != "auth-verification" {
		t.Fatal("token purposes must match existing signed tokens and stored metadata")
	}
}

func TestTokenCreationRejectsUnsupportedPurpose(t *testing.T) {
	for _, purpose := range []string{"", "session"} {
		t.Run(purpose, func(t *testing.T) {
			if _, err := newAuthToken(&User{ID: "test-user"}, []byte("test-key"), time.Hour, purpose); err == nil {
				t.Fatal("created a token with an unsupported purpose")
			}
		})
	}
}

func TestSignedTokenValidation(t *testing.T) {
	for _, purpose := range []string{ResetTokenPurpose, VerificationTokenPurpose} {
		for _, failure := range []string{
			"missing purpose", "unknown purpose", "wrong purpose", "missing user",
			"missing expiry", "null expiry", "zero expiry", "invalid expiry", "expired", "expired wrong key",
			"wrong key", "empty key", "HS384", "HS512", "unsigned",
			"future issued at", "future not before", "malformed",
		} {
			t.Run(purpose+"/"+failure, func(t *testing.T) {
				claims := jwt.MapClaims{"Id": "test-user", "purpose": purpose, "email": "person@example.invalid", "exp": time.Now().Add(time.Hour).Unix()}
				key := []byte("test-signing-key")
				var signingKey any = key
				var method jwt.SigningMethod = jwt.SigningMethodHS256
				switch failure {
				case "missing purpose":
					delete(claims, "purpose")
				case "unknown purpose":
					claims["purpose"] = "session"
				case "wrong purpose":
					claims["purpose"] = ResetTokenPurpose
					if purpose == ResetTokenPurpose {
						claims["purpose"] = VerificationTokenPurpose
					}
				case "missing user":
					delete(claims, "Id")
				case "missing expiry":
					delete(claims, "exp")
				case "null expiry":
					claims["exp"] = nil
				case "zero expiry":
					claims["exp"] = 0
				case "invalid expiry":
					claims["exp"] = "tomorrow"
				case "expired", "expired wrong key":
					claims["exp"] = time.Now().Add(-time.Hour).Unix()
				case "HS384":
					method = jwt.SigningMethodHS384
				case "HS512":
					method = jwt.SigningMethodHS512
				case "unsigned":
					method, signingKey = jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType
				case "future issued at":
					claims["iat"] = time.Now().Add(time.Hour).Unix()
				case "future not before":
					claims["nbf"] = time.Now().Add(time.Hour).Unix()
				}
				raw, err := jwt.NewWithClaims(method, claims).SignedString(signingKey)
				if err != nil {
					t.Fatal(err)
				}
				switch failure {
				case "wrong key", "expired wrong key":
					key = []byte("another-key")
				case "empty key":
					key = nil
				case "malformed":
					raw = "not-a-token"
				}
				if _, err := parseAuthToken(raw, key, purpose); err == nil {
					t.Fatal("invalid token accepted")
				} else if failure == "expired" {
					var expired *ErrTokenExpired
					if !errors.As(err, &expired) {
						t.Fatalf("error = %v, want ErrTokenExpired", err)
					}
				} else if failure == "expired wrong key" {
					var expired *ErrTokenExpired
					if errors.As(err, &expired) {
						t.Fatal("invalid signature was classified as an expired valid token")
					}
				}
			})
		}
	}
}

type tokenLookupRepository struct {
	AccountRepository
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
		for _, scenario := range []string{"success", "wrong signature", "wrong signed purpose", "empty token", "nil repository", "missing token", "token failure", "wrong stored account", "wrong stored hash", "wrong stored purpose", "stored expiry", "missing account", "account failure", "nil account", "wrong account", "changed email", "canceled", "account canceled"} {
			t.Run(purpose+"/"+scenario, func(t *testing.T) {
				account := &User{ID: "account", Email: "person@example.invalid"}
				key := []byte("test-signing-key")
				tokenPurpose := purpose
				if scenario == "wrong signed purpose" {
					tokenPurpose = ResetTokenPurpose
					if purpose == ResetTokenPurpose {
						tokenPurpose = VerificationTokenPurpose
					}
				}
				token, err := newAuthToken(account, key, time.Hour, tokenPurpose)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "wrong signature" {
					key = []byte("wrong-key")
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
							return TokenMetadata{}, NewErrTokenNotFound(nil)
						case "token failure":
							return TokenMetadata{}, failure
						case "wrong stored account":
							metadata.AccountID = "another-account"
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
							return nil, NewErrUserNotFound(nil)
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
				got, err := validate(ctx, repository, token.Token, key)
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
				case "wrong signature", "wrong signed purpose", "empty token", "nil repository":
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
			})
		}
	}
}

func TestValidateTokenExpiryDuringLookup(t *testing.T) {
	for _, purpose := range []string{ResetTokenPurpose, VerificationTokenPurpose} {
		for _, waitAt := range []string{"token", "account"} {
			for _, deadline := range []string{"signed", "stored"} {
				t.Run(purpose+"/"+waitAt+"/"+deadline, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						account := &User{ID: "account", Email: "person@example.invalid"}
						key := []byte("test-signing-key")
						lifetime := time.Hour
						if deadline == "signed" {
							lifetime = time.Second
						}
						token, err := newAuthToken(account, key, lifetime, purpose)
						if err != nil {
							t.Fatal(err)
						}
						metadata := token.Metadata()
						metadata.ExpiresAt = time.Now().Add(time.Hour)
						if deadline == "stored" {
							metadata.ExpiresAt = time.Now().Add(time.Second)
						}
						repository := tokenLookupRepository{
							token: func(context.Context, string, string) (TokenMetadata, error) {
								if waitAt == "token" {
									time.Sleep(time.Second)
								}
								return metadata, nil
							},
							account: func(context.Context, string) (*User, error) {
								if waitAt == "account" {
									time.Sleep(time.Second)
								}
								return account, nil
							},
						}
						got, err := validateToken(context.Background(), repository, token.Token, key, purpose)
						var expired *ErrTokenExpired
						if got != nil || !errors.As(err, &expired) {
							t.Fatalf("lookup accepted token at its expiry boundary: %v, %v", got, err)
						}
					})
				})
			}
		}
	}
}
