package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
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
				// Init requires a database reference but does not access it.
				Database:          &sqlx.DB{},
				AccountRepository: user.NewSQLiteAccountRepository(&sqlx.DB{}),
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
	lookup func(context.Context, string) (*user.User, error)
	create func(context.Context, *user.User) error
}

func (s emailAccountRepository) Create(ctx context.Context, u *user.User) error {
	return s.create(ctx, u)
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
				// Initialization must not query storage.
				Database:       &sqlx.DB{},
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
