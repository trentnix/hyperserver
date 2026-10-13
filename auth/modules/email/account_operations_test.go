package auth

import (
	"context"
	"errors"
	"html/template"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAuthenticateOperation(t *testing.T) {
	hash, err := password.HashPassword("TestPassword1!")
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("storage unavailable")
	for _, tc := range []struct {
		name       string
		hash       string
		missing    bool
		lookupErr  error
		credential string
		wantErr    error
	}{
		{"success", hash, false, nil, "TestPassword1!", nil},
		{"wrong password", hash, false, nil, "WrongPassword1!", errInvalidCredentials},
		{"missing password", "", false, nil, "TestPassword1!", errInvalidCredentials},
		{"corrupt hash", "invalid", false, nil, "TestPassword1!", errInvalidCredentials},
		{"nil account", "", true, nil, "TestPassword1!", errInvalidCredentials},
		{"not found", "", true, errors.Join(errors.New("lookup failed"), user.NewErrUserNotFound(nil)), "TestPassword1!", errInvalidCredentials},
		{"storage failure", hash, false, failure, "TestPassword1!", failure},
		{"canceled", hash, false, context.Canceled, "TestPassword1!", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.name == "canceled" {
				cancel()
			}
			account := &user.User{ID: "account", Email: "person@example.invalid", Password: tc.hash}
			calls := 0
			service := &EmailAuthService{accounts: emailAccountRepository{lookup: func(got context.Context, email string) (*user.User, error) {
				calls++
				if got != ctx || email != account.Email {
					t.Fatal("lookup lost context or email")
				}
				if tc.missing {
					return nil, tc.lookupErr
				}
				return account, tc.lookupErr
			}}}
			got, err := service.authenticate(ctx, account.Email, tc.credential)
			if calls != 1 || !errors.Is(err, tc.wantErr) {
				t.Fatalf("calls=%d, error=%v, want %v", calls, err, tc.wantErr)
			}
			if (err == nil && got != account) || (err != nil && got != nil) {
				t.Fatal("authentication returned the wrong account outcome")
			}
		})
	}
}

func TestPasswordOperations(t *testing.T) {
	const oldPassword, newPassword = "OldPassword1!", "NewPassword1!"
	hash, err := password.HashPassword(oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("storage unavailable")
	for _, operation := range []string{"change", "reset"} {
		scenarios := []string{"success", "missing account", "credential rejected", "hash failure", "storage failure", "canceled"}
		if operation == "reset" {
			scenarios = append(scenarios, "reuse allowed", "invalid token")
		}
		for _, scenario := range scenarios {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if scenario == "canceled" {
					cancel()
				}
				account := &user.User{ID: "account", Password: hash, SessionVersion: 1}
				before := *account
				key := "operation-test-key"
				token, err := user.NewAuthResetToken(account, []byte(key), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				current, replacement := oldPassword, newPassword
				if scenario == "credential rejected" {
					current, replacement = "WrongPassword1!", oldPassword
				}
				if scenario == "hash failure" {
					replacement = strings.Repeat("a", 73)
				}
				if scenario == "reuse allowed" {
					replacement = oldPassword
				}
				calls := 0
				write := func(got context.Context, u *user.User, encoded string) error {
					calls++
					if got != ctx || u != account || !password.CheckPasswordHash(replacement, encoded) {
						t.Fatal("password write lost context, account, or hashed credentials")
					}
					if scenario == "storage failure" {
						return failure
					}
					if err := got.Err(); err != nil {
						return err
					}
					u.Password, u.SessionVersion = encoded, u.SessionVersion+1
					return nil
				}
				accounts := emailAccountRepository{
					changePassword: write,
					resetPassword: func(got context.Context, u *user.User, authorization user.ResetAuthorization, encoded string) error {
						if authorization.TokenHash != token.TokenHash || authorization.AccountID != account.ID || authorization.ExpiresAt.Unix() != token.ExpiresAt.Unix() {
							t.Fatal("reset lost the exact token authorization")
						}
						return write(got, u, encoded)
					},
				}
				service := &EmailAuthService{accounts: accounts, config: &config.Config{Auth: config.AuthConfig{JwtKey: key}}}
				u := account
				if scenario == "missing account" {
					u = nil
				}
				rawToken := token.Token
				if scenario == "invalid token" {
					rawToken = "invalid-token"
				}
				if operation == "change" {
					err = service.changePassword(ctx, u, current, replacement)
				} else {
					err = service.resetPassword(ctx, u, rawToken, replacement, scenario != "reuse allowed")
				}
				success := scenario == "success" || scenario == "reuse allowed"
				wantCalls := 0
				if success || scenario == "storage failure" || scenario == "canceled" {
					wantCalls = 1
				}
				if calls != wantCalls || (err == nil) != success {
					t.Fatalf("calls=%d, error=%v", calls, err)
				}
				if success {
					if account.SessionVersion != before.SessionVersion+1 || !password.CheckPasswordHash(replacement, account.Password) {
						t.Fatal("operation lost the committed account changes")
					}
				} else if *account != before {
					t.Fatal("failed operation changed the account")
				}
				wantErr := map[string]error{"storage failure": failure, "canceled": context.Canceled}[scenario]
				if scenario == "credential rejected" {
					wantErr = errInvalidCredentials
					if operation == "reset" {
						wantErr = errPasswordReused
					}
				}
				if wantErr != nil && !errors.Is(err, wantErr) {
					t.Fatalf("error=%v, want %v", err, wantErr)
				}
			})
		}
	}
}

func TestAccountDeliveryOperations(t *testing.T) {
	for _, operation := range []string{"registration", "reset"} {
		for _, scenario := range []string{"success", "unknown", "nil account", "other provider", "lookup failure", "canceled", "storage failure", "mail failure", "mail unavailable"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if scenario == "canceled" {
					cancel()
				}
				account := &user.User{ID: "account", Email: "stored@example.invalid", RegistrationAuthType: AuthTypeEmail}
				if scenario == "other provider" {
					account.RegistrationAuthType = "other"
				}
				failure := errors.New("private storage or delivery failure")
				notFound := user.NewErrUserNotFound(nil)
				lookups, stored, sent := 0, 0, 0
				accounts := emailAccountRepository{
					lookup: func(got context.Context, email string) (*user.User, error) {
						lookups++
						if got != ctx || email != "submitted@example.invalid" {
							t.Fatal("lookup lost context or submitted address")
						}
						switch scenario {
						case "unknown":
							return nil, errors.Join(errors.New("lookup failed"), notFound)
						case "nil account":
							return nil, nil
						case "lookup failure":
							return nil, failure
						}
						return account, got.Err()
					},
					createToken: func(got context.Context, metadata user.TokenMetadata) error {
						if got != ctx || metadata.AccountID != account.ID {
							t.Fatal("token storage lost context or account")
						}
						if scenario == "storage failure" {
							return failure
						}
						stored++
						return nil
					},
				}
				client, err := messaging.NewMailClientWithSender("sender@example.invalid", accountMailSender(func(got context.Context, message messaging.MailMessage) error {
					if got != ctx || message.To != account.Email || stored != 1 {
						t.Fatal("delivery lost context or stored address, or preceded token storage")
					}
					sent++
					if scenario == "mail failure" {
						return failure
					}
					return nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "mail unavailable" {
					client = nil
				}
				email := template.Must(template.New("email").Parse("{{.URL}}"))
				service := &EmailAuthService{accounts: accounts, mailClient: client, verificationEmail: email, resetEmail: email,
					config: &config.Config{Auth: config.AuthConfig{JwtKey: "test-key", VerificationTokenExpiration: time.Hour}}}
				link := url.URL{Scheme: "https", Host: "example.invalid", Path: emailResetPath}
				if operation == "registration" {
					err = service.sendRegistrationVerification(ctx, "submitted@example.invalid", link)
				} else {
					err = service.requestPasswordReset(ctx, "submitted@example.invalid", time.Hour, link)
				}
				wantStored, wantSent := 0, 0
				var wantErr error
				switch scenario {
				case "success", "other provider", "mail failure":
					wantStored, wantSent = 1, 1
					if scenario == "other provider" && operation == "reset" {
						wantStored, wantSent = 0, 0
					}
					if scenario == "mail failure" {
						wantErr = failure
					}
				case "lookup failure", "storage failure":
					wantErr = failure
				case "canceled":
					wantErr = context.Canceled
				case "mail unavailable":
					wantErr = messaging.ErrMailUnavailable
				}
				if operation == "registration" && (scenario == "unknown" || scenario == "nil account") {
					var missing *user.ErrUserNotFound
					if !errors.As(err, &missing) {
						t.Fatalf("expected missing registered account, got %v", err)
					}
				} else if !errors.Is(err, wantErr) {
					t.Fatalf("error=%v, want %v", err, wantErr)
				}
				if lookups != 1 || stored != wantStored || sent != wantSent {
					t.Fatalf("error=%v, lookups=%d, stored=%d, sent=%d", err, lookups, stored, sent)
				}
			})
		}
	}
}
