package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type emailSessionStore struct{ session.SessionStore }

func (emailSessionStore) IsEnabled() bool { return true }
func (s emailSessionStore) Get(_ *http.Request, name string) (*session.Session, error) {
	return session.NewSession(s, name), nil
}

type emailIdentityStore struct {
	emailSessionStore
	session.RotatingStore
}

func emailSessions(t *testing.T, types map[string]string) *session.SessionManager {
	t.Helper()
	manager, err := session.NewSessionManager(t.Context(), types, map[string]session.StoreFactory{
		"memory":   func(context.Context) (session.SessionStore, error) { return emailSessionStore{}, nil },
		"identity": func(context.Context) (session.SessionStore, error) { return emailIdentityStore{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	return manager
}

func TestEmailFormsUseStartupTemplates(t *testing.T) {
	cm := content.NewContentManager()
	layouts := fstest.MapFS{"layout.html": {Data: []byte(`{{renderPartial .PartialName .}}`)}}
	cm.AddPageLayout(layouts, "layout.html")
	cm.AddHtmxLayout(layouts, "layout.html")
	cm.HandleError = func(_ http.ResponseWriter, _ *http.Request, message string, err error, _ int) {
		t.Fatalf("form failed: %s: %v", message, err)
	}
	sessions := emailSessions(t, map[string]string{"default": "memory"})
	service := Provider(&emailAccountRepository{}, sessions, nil).New().(*EmailAuthService)
	app := &server.ApplicationServer{ContentManager: cm, Config: &config.Config{Auth: config.AuthConfig{
		Enabled: true, RegistrationEnabled: true, JwtKey: "test-key000000000000000000000000",
		Services: map[string]map[string]string{"email": {"enabled": "true"}},
	}}}
	if err := service.Init(app); err != nil {
		t.Fatal(err)
	}
	// Neither the first request nor later requests may reopen the selected files.
	service.TemplateFS = fstest.MapFS{}
	for name, handler := range map[string]http.HandlerFunc{
		"login": service.GetLogin, "registration": service.GetRegister,
		"reset request": service.GetResetRequest, "change": service.GetChange,
		"reset": func(w http.ResponseWriter, r *http.Request) { service.GetReset(w, r, "test-token") },
	} {
		for _, htmx := range []bool{false, true} {
			for range 2 {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				r = session.AddSessionManagerToRequestContext(r, sessions)
				w := httptest.NewRecorder()
				handler(w, r)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<form") {
					t.Fatalf("%s htmx=%t: status=%d body=%s", name, htmx, w.Code, w.Body.String())
				}
			}
		}
	}
}

func TestEmailAuthModuleRequiresFlashSession(t *testing.T) {
	for _, selection := range []string{"missing", "named", "default"} {
		t.Run(selection, func(t *testing.T) {
			cfg := config.Config{Auth: config.AuthConfig{
				Enabled: true, JwtKey: "test-key000000000000000000000000", VerificationTokenExpiration: time.Hour,
				ResetTokenExpiration: time.Hour, Services: map[string]map[string]string{"email": {"enabled": "true"}},
			}}
			app, err := server.NewApplicationServer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			accounts := &emailAccountRepository{}
			layouts := fstest.MapFS{"layout.html": {Data: []byte(`{{renderPartial .PartialName .}}`)}}
			app.ContentManager.AddPageLayout(layouts, "layout.html")
			app.ContentManager.AddHtmxLayout(layouts, "layout.html")
			types := map[string]string{"auth-user-session": "identity", session.AuthSession: "memory"}
			switch selection {
			case "named":
				types["hs-message-session"] = "memory"
			case "default":
				types["default"] = "memory"
			}
			sessions := emailSessions(t, types)
			module := auth_services.Module(accounts, sessions, []auth_services.Descriptor{Provider(accounts, sessions, nil)}).New()

			err = module.Init(t.Context(), app)
			if selection == "missing" {
				if !errors.Is(err, session.ErrSessionStoreNotFound) || !strings.Contains(err.Error(), "flash") {
					t.Fatalf("initialization error = %v, want missing flash session", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := module.Routes(routing.NewRoutes(app.Web)); err != nil {
				t.Fatal(err)
			}

			for _, htmx := range []bool{false, true} {
				r := httptest.NewRequest(http.MethodGet, "/auth/login/email", nil)
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				r = session.AddSessionManagerToRequestContext(r, sessions)
				w := httptest.NewRecorder()
				app.Web.ServeHTTP(w, r)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `name="password"`) {
					t.Fatalf("login response (HTMX=%t) = %d %q, want a login form", htmx, w.Code, w.Body.String())
				}
			}
		})
	}
}

type failingAuthMessageStore struct {
	session.SessionStore
	err error
}

func (s failingAuthMessageStore) Save(http.ResponseWriter, *http.Request, *session.Session) error {
	return s.err
}
func (s failingAuthMessageStore) End(http.ResponseWriter, *http.Request, *session.Session) error {
	return s.err
}

func TestAuthFormsReportMessageRemovalFailure(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true, RegistrationEnabled: true}}
	manager, err := session.NewSessionManager(t.Context(), map[string]string{"default": "cookie"}, map[string]session.StoreFactory{
		"cookie": func(context.Context) (session.SessionStore, error) {
			return cookie.NewCookieStore(cookie.Options{
				SigningKey: "message-removal-test-key00000000000000000000000000000000", TokenLifetime: time.Hour, CookieLifetime: time.Hour,
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	service := &EmailAuthService{TemplateFS: emailAuthTemplatesFS, config: cfg, contentManager: content.NewContentManager()}
	if err := service.prepareTemplates(); err != nil {
		t.Fatal(err)
	}
	handlers := map[string]http.HandlerFunc{
		"login": service.GetLogin, "registration": service.GetRegister, "reset request": service.GetResetRequest,
		"reset": func(w http.ResponseWriter, r *http.Request) { service.GetReset(w, r, "test-token") }, "change": service.GetChange,
	}
	for name, handler := range handlers {
		for _, operation := range []string{"save", "end"} {
			for _, htmx := range []bool{false, true} {
				t.Run(name+"/"+operation+"/htmx="+fmt.Sprint(htmx), func(t *testing.T) {
					r := httptest.NewRequest(http.MethodGet, "/", nil)
					if htmx {
						r.Header.Set("HX-Request", "true")
					}
					r = session.AddSessionManagerToRequestContext(r, manager)
					if err := flash.AddMessage(httptest.NewRecorder(), r, "pending", flash.AuthMessages); err != nil {
						t.Fatal(err)
					}
					if operation == "save" {
						if err := flash.AddSuccessNotification(httptest.NewRecorder(), r, "other"); err != nil {
							t.Fatal(err)
						}
					}
					value, err := session.Get(r, "hs-message-session")
					if err != nil {
						t.Fatal(err)
					}
					failure := errors.New("message storage failure")
					value.Store = failingAuthMessageStore{SessionStore: value.Store, err: failure}
					calls := 0
					service.contentManager.HandleError = func(w http.ResponseWriter, r *http.Request, message string, err error, status int) {
						calls++
						if !errors.Is(err, failure) || message != "Unable to load authentication messages" || status != http.StatusInternalServerError {
							t.Fatalf("wrong error: %q, %v, %d", message, err, status)
						}
						http.Error(w, message, status)
					}
					w := httptest.NewRecorder()
					handler(w, r)
					if calls != 1 || w.Code != http.StatusInternalServerError || w.Body.String() != "Unable to load authentication messages\n" {
						t.Fatalf("form ignored removal failure: calls=%d, status=%d, body=%q", calls, w.Code, w.Body.String())
					}
				})
			}
		}
	}
}

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
		service.Register(w, r)
		if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != "Registration is not available." {
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
		{name: "missing login form", path: emailLoginFormTemplate},
		{name: "invalid registration form", path: emailRegisterFormTemplate, template: "{{if}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()

			files := map[string]string{
				emailLoginButtonTemplateName:    "Login",
				emailRegisterButtonTemplateName: "Register",
				emailVerificationTemplate:       "Verify {{.Name}}: {{.URL}}",
				emailResetTemplate:              "Reset {{.Name}}: {{.URL}}",
			}
			for _, name := range []string{emailLoginFormTemplate, emailRegisterFormTemplate, emailResetRequestFormTemplate, emailResetPasswordFormTemplate, emailChangePasswordFormTemplate, "templates/html/partials/registration-pending.html"} {
				data, err := fs.ReadFile(emailAuthTemplatesFS, name)
				if err != nil {
					t.Fatal(err)
				}
				files[name] = string(data)
			}
			if tc.template != "" {
				files[tc.path] = tc.template
			} else {
				delete(files, tc.path)
			}
			for path, body := range files {
				if err := os.MkdirAll(filepath.Join(root, filepath.Dir(path)), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}

			app := &server.ApplicationServer{
				Config: &config.Config{Auth: config.AuthConfig{
					JwtKey:   "test-key000000000000000000000000",
					Services: map[string]map[string]string{"email": {"enabled": "true"}},
				}},
				ContentManager: &content.ContentManagerService{},
			}
			service := Provider(&emailAccountRepository{}, emailSessions(t, map[string]string{"default": "memory"}), nil).New().(*EmailAuthService)
			service.TemplateFS = os.DirFS(root)
			initErr := service.Init(app)
			cause := errors.Unwrap(initErr)
			want := filepath.Base(tc.path)
			if cause == nil || (!strings.Contains(initErr.Error(), want) && !strings.Contains(cause.Error(), want)) {
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

func (s emailAccountRepository) InsertToken(ctx context.Context, metadata user.TokenMetadata) error {
	return s.createToken(ctx, metadata)
}

func (s emailAccountRepository) ChangePassword(ctx context.Context, u *user.User, hash string) error {
	return s.changePassword(ctx, u, hash)
}

func (s emailAccountRepository) ResetPassword(ctx context.Context, u *user.User, authorization user.ResetAuthorization, hash string) error {
	return s.resetPassword(ctx, u, authorization, hash)
}

func TestEmailAuthInitAccountRepository(t *testing.T) {
	for _, scenario := range []string{"supplied", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			app := &server.ApplicationServer{
				Config: &config.Config{Auth: config.AuthConfig{
					Enabled: true, JwtKey: "test-key000000000000000000000000",
					Services: map[string]map[string]string{"email": {"enabled": "true"}},
				}},
				// A supplied repository needs no SQL pool and must not be queried during initialization.
				ContentManager: content.NewContentManager(),
			}
			reader := &emailAccountRepository{}
			var accounts user.AccountRepository
			if scenario == "supplied" {
				accounts = reader
			}
			service := Provider(accounts, emailSessions(t, map[string]string{"default": "memory"}), nil).New().(*EmailAuthService)
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
					return nil, errors.Join(errors.New("lookup failed"), user.ErrUserNotFound)
				case "storage failure":
					return nil, errors.New("private storage failure")
				case "canceled":
					return nil, got.Err()
				}
				return account, nil
			}}
			cm := content.NewContentManager()
			cm.AddHtmxLayout(fstest.MapFS{"partial.html": &fstest.MapFile{Data: []byte(`{{renderPartial .PartialName .}}`)}}, "partial.html")
			service := &EmailAuthService{TemplateFS: emailAuthTemplatesFS, accounts: reader, contentManager: cm}
			if err := service.prepareTemplates(); err != nil {
				t.Fatal(err)
			}
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
		TemplateFS: emailAuthTemplatesFS,
		accounts:   accounts,
		config: &config.Config{Auth: config.AuthConfig{
			Enabled: true, RegistrationEnabled: true,
		}},
		contentManager: content.NewContentManager(),
	}
	service.contentManager.AuthURL = "/login"
	if err := service.prepareTemplates(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	service.Register(w, r)
	if calls != 1 || w.Code != http.StatusSeeOther || w.Header().Get("Location") != service.contentManager.AuthURL {
		t.Fatalf("registration bypassed the account repository: calls=%d", calls)
	}
}

func TestChangeUsesAccountRepository(t *testing.T) {
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
			cm.AddHtmxLayout(fstest.MapFS{"partial.html": &fstest.MapFile{Data: []byte(`{{renderPartial .PartialName .}}`)}}, "partial.html")
			// No SQL pool: password changes must use the supplied repository.
			service := &EmailAuthService{TemplateFS: emailAuthTemplatesFS, accounts: accounts, contentManager: cm}
			if err := service.prepareTemplates(); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			service.Change(w, r, account)
			wantCalls := 1
			if scenario == "wrong password" || scenario == "invalid confirmation" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("repository calls=%d, want %d", calls, wantCalls)
			}

			if scenario == "success" {
				cookies := w.Result().Cookies()
				if account.SessionVersion != before.SessionVersion+1 || !password.CheckPasswordHash(form.Get("newPassword"), account.Password) {
					t.Fatal("successful change did not preserve the committed credentials")
				}
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

func TestChangeRendersLoggedOutLayout(t *testing.T) {
	hash, err := password.HashPassword("OldPassword1!")
	if err != nil {
		t.Fatal(err)
	}
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%v", htmx), func(t *testing.T) {
			account := &user.User{ID: "account", Password: hash, SessionVersion: 1}
			accounts := emailAccountRepository{changePassword: func(_ context.Context, u *user.User, hash string) error {
				u.Password = hash
				u.SessionVersion++
				return nil
			}}
			cm := content.NewContentManager()
			cm.BuildLayoutData = func(r *http.Request) any { return user.GetUserFromContext(r.Context()) != nil }
			layout := fstest.MapFS{"layout.html": {Data: []byte(`signed-in={{.LayoutData}} {{renderPartial .PartialName .}}`)}}
			cm.AddPageLayout(layout, "layout.html")
			cm.AddHtmxLayout(layout, "layout.html")
			service := &EmailAuthService{TemplateFS: emailAuthTemplatesFS, accounts: accounts, contentManager: cm}
			if err := service.prepareTemplates(); err != nil {
				t.Fatal(err)
			}
			values := url.Values{"oldPassword": {"OldPassword1!"}, "newPassword": {"NewPassword1!"}, "newPasswordMatch": {"NewPassword1!"}}
			r := httptest.NewRequest("POST", emailChangePath, strings.NewReader(values.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if htmx {
				r.Header.Set("HX-Request", "true")
			}
			r = user.AddUserToRequestContext(r, account)
			w := httptest.NewRecorder()
			service.Change(w, r, account)
			if w.Code != 200 || user.GetUserFromContext(r.Context()) != nil || !strings.Contains(w.Body.String(), "signed-in=false") || !strings.Contains(w.Body.String(), "Your password has been changed") {
				t.Fatalf("stale success response: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestResetUsesAccountRepository(t *testing.T) {
	for _, scenario := range []string{"success", "storage failure", "consumed", "expired", "changed account", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			failure := map[string]error{
				"storage failure": errors.New("private storage failure"),
				"consumed":        user.ErrTokenNotFound,
				"expired":         user.ErrTokenExpired,
				"changed account": user.ErrUserChanged,
				"canceled":        context.Canceled,
			}[scenario]
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
				return failure
			}}
			cm := content.NewContentManager()
			cm.AddHtmxLayout(fstest.MapFS{"partial.html": &fstest.MapFile{Data: []byte(`{{renderPartial .PartialName .}}`)}}, "partial.html")
			// No SQL pool: reset submission must use the supplied repository.
			cm.AuthURL = "/login"
			cm.HandleError = func(w http.ResponseWriter, r *http.Request, message string, err error, status int) {
				if err != nil && (strings.Contains(err.Error(), "private storage failure") || strings.Contains(err.Error(), token.Token)) {
					t.Fatal("reset passed sensitive details to the error renderer")
				}
				http.Error(w, message, status)
			}
			service := &EmailAuthService{
				TemplateFS: emailAuthTemplatesFS,
				accounts:   accounts, contentManager: cm,
				config: &config.Config{Auth: config.AuthConfig{JwtKey: key}},
			}
			if err := service.prepareTemplates(); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			service.Reset(w, r, account, token.Token, false)
			if calls != 1 {
				t.Fatalf("repository calls=%d, want 1", calls)
			}
			if failure == nil && (w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != cm.AuthURL) {
				t.Fatalf("successful reset did not redirect: %d %v", w.Code, w.Header())
			}
			if failure != nil {
				want := http.StatusInternalServerError
				if user.IsTokenRejection(failure) {
					want = http.StatusBadRequest
				}
				if w.Code != want || w.Header().Get("HX-Redirect") != "" || !strings.Contains(w.Body.String(), "Unable to reset password") || strings.Contains(w.Body.String(), "private storage failure") {
					t.Fatalf("failed reset rendered an incorrect error: %d %s", w.Code, w.Body.String())
				}
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
