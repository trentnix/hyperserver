package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestDisabledRegistration(t *testing.T) {
	for _, cfg := range []config.AuthConfig{
		{Enabled: true, RegistrationEnabled: false},
		{Enabled: false, RegistrationEnabled: true},
	} {
		service := &EmailAuthService{config: &config.Config{Auth: cfg}, registerButton: "registration link"}
		if service.GetRegisterButton() != "" {
			t.Error("disabled registration returned a link")
		}
		r := httptest.NewRequest(http.MethodPost, "/auth/register/email", nil)
		w := httptest.NewRecorder()
		service.GetRegister(w, r)
		if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != "Registration is not available." {
			t.Errorf("registration form returned %d %q, want 403 and an explanation", w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		if service.Register(w, r) || w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != "Registration is not available." {
			t.Errorf("registration submission returned %d %q, want 403 and an explanation", w.Code, w.Body.String())
		}
	}
}

func TestInitRequiresAccountEmailTemplates(t *testing.T) {
	for _, tc := range []struct {
		name, path, template string
	}{
		{name: "missing verification", path: emailVerificationTemplate},
		{name: "invalid verification", path: emailVerificationTemplate, template: "{{if}}"},
		{name: "missing reset", path: emailResetTemplate},
		{name: "invalid reset", path: emailResetTemplate, template: "{{if}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalDirectory, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if err := os.Chdir(root); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chdir(originalDirectory); err != nil {
					t.Error(err)
				}
			})

			files := map[string]string{
				emailLoginButtonTemplateName:    "Login",
				emailRegisterButtonTemplateName: "Register",
				emailVerificationTemplate:       "Verify {{.Name}}: {{.URL}}",
				emailResetTemplate:              "Reset {{.Name}}: {{.URL}}",
			}
			if tc.template != "" {
				files[tc.path] = tc.template
			} else {
				delete(files, tc.path)
			}
			for path, body := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}

			app := &server.ApplicationServer{
				Config: &config.Config{Auth: config.AuthConfig{
					JwtKey:   "test-key",
					Services: map[string]map[string]string{"email": {"enabled": "true"}},
				}},
				AccountRepository: &emailAccountRepository{},
				ContentManager:    &content.ContentManagerService{},
			}
			service := &EmailAuthService{}
			initErr := service.Init(app)
			cause := errors.Unwrap(initErr)
			want := filepath.Base(tc.path)
			if cause == nil || !strings.Contains(cause.Error(), want) {
				t.Fatalf("Init error = %v, cause = %v, want %s failure", initErr, cause, want)
			}
		})
	}
}

type emailAccountRepository struct {
	user.AccountRepository
	lookup         func(context.Context, string) (*user.User, error)
	create         func(context.Context, *user.User) error
	createToken    func(context.Context, user.TokenMetadata) error
	changePassword func(context.Context, *user.User, string) error
	resetPassword  func(context.Context, *user.User, user.ResetAuthorization, string) error
}

func (s emailAccountRepository) Create(ctx context.Context, u *user.User) error {
	return s.create(ctx, u)
}

func (s emailAccountRepository) CreateToken(ctx context.Context, metadata user.TokenMetadata) error {
	return s.createToken(ctx, metadata)
}

func (s emailAccountRepository) ChangePassword(ctx context.Context, u *user.User, hash string) error {
	return s.changePassword(ctx, u, hash)
}

func (s emailAccountRepository) ResetPassword(ctx context.Context, u *user.User, authorization user.ResetAuthorization, hash string) error {
	return s.resetPassword(ctx, u, authorization, hash)
}

func TestEmailAuthInitAccountRepository(t *testing.T) {
	t.Chdir("../../..")
	for _, scenario := range []string{"supplied", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			app := &server.ApplicationServer{
				Config: &config.Config{Auth: config.AuthConfig{
					Enabled: true, JwtKey: "test-key",
					Services: map[string]map[string]string{"email": {"enabled": "true"}},
				}},
				// A supplied repository needs no SQL pool and must not be queried during initialization.
				ContentManager: content.NewContentManager(),
			}
			reader := &emailAccountRepository{}
			if scenario == "supplied" {
				app.AccountRepository = reader
			}
			service := &EmailAuthService{}
			err := service.Init(app)
			if scenario == "missing" {
				cause := errors.Unwrap(err)
				if cause == nil || cause.Error() != "the account repository is not configured" {
					t.Fatalf("missing account repository error = %v", err)
				}
				return
			}
			if err != nil || service.accounts != reader {
				t.Fatalf("initialization did not preserve the application-supplied account repository: %v", err)
			}
		})
	}
}

func (s emailAccountRepository) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	return s.lookup(ctx, email)
}

func TestLoginUsesAccountRepository(t *testing.T) {
	t.Chdir("../../..")
	hash, err := password.HashPassword("TestPassword1!")
	if err != nil {
		t.Fatal(err)
	}
	account := &user.User{ID: "account", Email: "person@example.invalid", Password: hash, SessionVersion: 1}
	for _, scenario := range []string{"success", "missing", "storage failure", "canceled", "wrong password"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "canceled" {
				cancel()
			}
			calls := 0
			reader := emailAccountRepository{lookup: func(got context.Context, email string) (*user.User, error) {
				calls++
				if got != ctx || email != account.Email {
					t.Fatal("lookup lost the request context or email")
				}
				switch scenario {
				case "missing":
					return nil, errors.Join(errors.New("lookup failed"), user.NewErrUserNotFound(nil))
				case "storage failure":
					return nil, errors.New("private storage failure")
				case "canceled":
					return nil, got.Err()
				}
				return account, nil
			}}
			cm := content.NewContentManager()
			cm.AddHtmxLayout("modules/site/templates/html/layouts/partial.html")
			service := &EmailAuthService{accounts: reader, contentManager: cm}
			form := url.Values{"email": {account.Email}, "password": {"TestPassword1!"}}
			if scenario == "wrong password" {
				form.Set("password", "WrongPassword1!")
			}
			r := httptest.NewRequest(http.MethodPost, emailLoginPath, strings.NewReader(form.Encode())).WithContext(ctx)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("HX-Request", "true")
			w := httptest.NewRecorder()
			got := service.Login(w, r)
			if calls != 1 {
				t.Fatalf("lookup calls = %d, want 1", calls)
			}
			if scenario == "success" {
				if got != account {
					t.Fatal("successful reader lookup did not authenticate")
				}
				return
			}
			want := "Invalid login/password"
			if scenario == "storage failure" || scenario == "canceled" {
				want = "The user specified could not be retrieved from the database"
			}
			if got != nil || !strings.Contains(w.Body.String(), want) || strings.Contains(w.Body.String(), "private storage failure") {
				t.Fatalf("failed login authenticated or rendered an incorrect error: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRegisterUsesAccountRepository(t *testing.T) {
	t.Chdir("../../..")
	form := url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"}}
	r := httptest.NewRequest(http.MethodPost, emailRegisterPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	calls := 0
	accounts := emailAccountRepository{create: func(ctx context.Context, u *user.User) error {
		calls++
		if ctx != r.Context() || u.Email != form.Get("email") || !password.CheckPasswordHash(form.Get("password"), u.Password) {
			t.Fatal("registration did not pass its context and hashed credentials to storage")
		}
		return nil
	}}
	// No SQL pool or mail client: this request only requires the account repository.
	service := &EmailAuthService{
		accounts: accounts,
		config: &config.Config{Auth: config.AuthConfig{
			Enabled: true, RegistrationEnabled: true,
		}},
		contentManager: content.NewContentManager(),
	}
	if !service.Register(httptest.NewRecorder(), r) || calls != 1 {
		t.Fatalf("registration bypassed the account repository: calls=%d", calls)
	}
}

func TestChangeUsesAccountRepository(t *testing.T) {
	t.Chdir("../../..")
	hash, err := password.HashPassword("OldPassword1!")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"success", "storage failure", "canceled", "wrong password", "invalid confirmation"} {
		t.Run(scenario, func(t *testing.T) {
			account := &user.User{ID: "account", Password: hash, SessionVersion: 1}
			before := *account
			form := url.Values{"oldPassword": {"OldPassword1!"}, "newPassword": {"NewPassword1!"}, "newPasswordMatch": {"NewPassword1!"}}
			if scenario == "wrong password" {
				form.Set("oldPassword", "WrongPassword1!")
			}
			if scenario == "invalid confirmation" {
				form.Set("newPasswordMatch", "DifferentPassword1!")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "canceled" {
				cancel()
			}
			r := httptest.NewRequest(http.MethodPost, emailChangePath, strings.NewReader(form.Encode())).WithContext(ctx)
			r = user.AddUserToRequestContext(r, account)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("HX-Request", "true")
			calls := 0
			accounts := emailAccountRepository{changePassword: func(ctx context.Context, u *user.User, hash string) error {
				calls++
				if ctx != r.Context() || u != account || !password.CheckPasswordHash(form.Get("newPassword"), hash) {
					t.Fatal("password change lost its context, account, or hashed password")
				}
				if scenario == "storage failure" {
					return errors.New("private storage failure")
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				u.Password = hash
				u.SessionVersion++
				return nil
			}}
			cm := content.NewContentManager()
			cm.AddHtmxLayout("modules/site/templates/html/layouts/partial.html")
			// No SQL pool: password changes must use the supplied repository.
			service := &EmailAuthService{accounts: accounts, contentManager: cm}
			w := httptest.NewRecorder()
			changed := service.Change(w, r, account)
			wantCalls := 1
			if scenario == "wrong password" || scenario == "invalid confirmation" {
				wantCalls = 0
			}
			if calls != wantCalls || changed != (scenario == "success") {
				t.Fatalf("changed=%t, repository calls=%d, want %d", changed, calls, wantCalls)
			}

			if scenario == "success" {
				cookies := w.Result().Cookies()
				if user.GetUserFromContext(r.Context()) != nil || len(cookies) != 1 || cookies[0].MaxAge != -1 || !strings.Contains(w.Body.String(), "Your password has been changed. Please log in again.") {
					t.Fatalf("successful change did not clear authentication and render confirmation: %s", w.Body.String())
				}
				return
			}
			if *account != before || user.GetUserFromContext(r.Context()) != account || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "private storage failure") {
				t.Fatal("failed change modified authentication or exposed a storage error")
			}
			if wantCalls == 1 && !strings.Contains(w.Body.String(), "unable to change your password") {
				t.Fatalf("repository failure did not render an error: %s", w.Body.String())
			}
		})
	}
}

func TestResetUsesAccountRepository(t *testing.T) {
	t.Chdir("../../..")
	for _, scenario := range []string{"success", "storage failure"} {
		t.Run(scenario, func(t *testing.T) {
			fail := scenario == "storage failure"
			account := &user.User{ID: "account", SessionVersion: 1}
			key := "test-signing-key"
			token, err := user.NewAuthResetToken(account, []byte(key), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			form := url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}
			r := httptest.NewRequest(http.MethodPost, emailResetPath, strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("HX-Request", "true")
			calls := 0
			accounts := emailAccountRepository{resetPassword: func(ctx context.Context, u *user.User, authorization user.ResetAuthorization, hash string) error {
				calls++
				if ctx != r.Context() || u != account || authorization.AccountID != account.ID || authorization.TokenHash != token.TokenHash || !password.CheckPasswordHash(form.Get("password"), hash) {
					t.Fatal("reset did not pass the request context, account, token identity, and hashed password")
				}
				if fail {
					return errors.New("private storage failure")
				}
				return nil
			}}
			cm := content.NewContentManager()
			cm.AddHtmxLayout("modules/site/templates/html/layouts/partial.html")
			// No SQL pool: reset submission must use the supplied repository.
			service := &EmailAuthService{
				accounts: accounts, contentManager: cm,
				config: &config.Config{Auth: config.AuthConfig{JwtKey: key}},
			}
			w := httptest.NewRecorder()
			if got := service.Reset(w, r, account, token.Token, false); got == fail || calls != 1 {
				t.Fatalf("reset success=%t, repository calls=%d", got, calls)
			}
			if fail && (!strings.Contains(w.Body.String(), "Unable to reset password") || strings.Contains(w.Body.String(), "private storage failure")) {
				t.Fatalf("failed reset rendered an incorrect error: %s", w.Body.String())
			}
		})
	}
}

type accountMailSender func(context.Context, messaging.MailMessage) error

func (send accountMailSender) Send(ctx context.Context, message messaging.MailMessage) error {
	return send(ctx, message)
}

func TestAccountEmailStoresTokenBeforeDelivery(t *testing.T) {
	for _, purpose := range []string{"auth-reset", "auth-verification"} {
		for _, scenario := range []string{"success", "storage failure", "mail failure", "canceled"} {
			t.Run(purpose+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				account := &user.User{ID: "account", Email: "person@example.invalid"}
				key := "test-signing-key"
				failure := errors.New("test failure")
				attempt, storageCalls, mailCalls := 0, 0, 0
				var saved []user.TokenMetadata
				accounts := emailAccountRepository{createToken: func(got context.Context, metadata user.TokenMetadata) error {
					storageCalls++
					if got != ctx || metadata.AccountID != account.ID || metadata.Purpose != purpose || !metadata.ExpiresAt.After(time.Now()) {
						t.Fatal("token storage lost its context or metadata")
					}
					if err := got.Err(); err != nil {
						return err
					}
					if attempt == 1 && scenario == "storage failure" {
						return failure
					}
					saved = append(saved, metadata)
					return nil
				}}
				sender := accountMailSender(func(got context.Context, message messaging.MailMessage) error {
					mailCalls++
					if got != ctx || message.To != account.Email || len(saved) != attempt+1 {
						t.Fatal("delivery lost its context or recipient, or preceded token storage")
					}
					link, err := url.Parse(message.Body)
					if err != nil {
						t.Fatal(err)
					}
					wantPath := "/auth/reset/email"
					if purpose == "auth-verification" {
						wantPath = "/auth/verify"
					}
					if link.Scheme != "https" || link.Host != "example.invalid" || link.Path != wantPath {
						t.Fatalf("wrong account link: %s", link)
					}
					raw := link.Query().Get("token")
					claims := &user.VerificationClaims{}
					if _, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return []byte(key), nil }, jwt.WithExpirationRequired(), jwt.WithValidMethods([]string{"HS256"})); err != nil {
						t.Fatal(err)
					}
					hash := sha256.Sum256([]byte(raw))
					metadata := saved[attempt]
					if metadata.TokenHash != hex.EncodeToString(hash[:]) || claims.Id != account.ID || claims.Purpose != purpose || claims.ExpiresAt.Unix() != metadata.ExpiresAt.Unix() || (purpose == "auth-verification" && claims.Email != account.Email) {
						t.Fatal("emailed token does not match stored metadata or recipient")
					}
					if attempt == 1 && scenario == "mail failure" {
						return failure
					}
					return nil
				})
				client, err := messaging.NewMailClientWithSender("sender@example.invalid", sender)
				if err != nil {
					t.Fatal(err)
				}
				email := template.Must(template.New("email").Parse("{{.URL}}"))
				// No SQL pool: both delivery flows must use the supplied repository.
				service := &EmailAuthService{
					accounts: accounts, mailClient: client, verificationEmail: email, resetEmail: email,
					config: &config.Config{Auth: config.AuthConfig{JwtKey: key, VerificationTokenExpiration: time.Hour}},
				}
				send := func() error {
					link := url.URL{Scheme: "https", Host: "example.invalid", Path: "/auth/reset/email"}
					if purpose == "auth-verification" {
						return service.SendVerificationEmail(ctx, account, link)
					}
					return service.sendResetEmail(ctx, account, time.Hour, link)
				}
				if err := send(); err != nil {
					t.Fatal(err)
				}
				first := saved[0]
				attempt = 1
				var wantErr error
				wantStored, wantMail := 2, 2
				switch scenario {
				case "storage failure":
					wantErr, wantStored, wantMail = failure, 1, 1
				case "mail failure":
					wantErr = failure
				case "canceled":
					cancel()
					wantErr, wantStored, wantMail = context.Canceled, 1, 1
				}
				if err := send(); !errors.Is(err, wantErr) || storageCalls != 2 || len(saved) != wantStored || mailCalls != wantMail {
					t.Fatalf("resend error=%v, storage calls=%d, stored=%d, mail calls=%d", err, storageCalls, len(saved), mailCalls)
				}
				if saved[0] != first || (len(saved) == 2 && saved[1].TokenHash == first.TokenHash) {
					t.Fatal("resend changed or reused the first token")
				}
			})
		}
	}
}
