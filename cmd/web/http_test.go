package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// HTTP scenarios run in separate processes because module instances and database
// initialization currently use package globals. Each scenario owns its database,
// configuration, mail fake, and browser cookies. No network listener is started.
// Remove process isolation when those runtime globals become application-owned.
func runHTTPScenario(t *testing.T, scenario func(*httpHarness)) {
	t.Helper()
	if os.Getenv("HS_HTTP_TEST_CASE") == t.Name() {
		// These overrides would fail if the harness accidentally loaded runtime
		// configuration instead of using its explicit fixture.
		t.Setenv("HYPERSERVER_DATABASE_CONNECTION", filepath.Join(t.TempDir(), "missing", "developer.db"))
		t.Setenv("HYPERSERVER_AUTH_JWTKEY", "")
		t.Setenv("HYPERSERVER_HTTP_SESSION_JWTKEY", "")
		scenario(newHTTPHarness(t))
		if !t.Failed() {
			fmt.Println("HTTP scenario passed")
		}
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "HYPERSERVER_") && !strings.HasPrefix(entry, "HS_HTTP_TEST_CASE=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "HS_HTTP_TEST_CASE="+t.Name())
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("HTTP scenario timed out: %s", output)
	}
	if err != nil || !strings.Contains(string(output), "HTTP scenario passed") {
		t.Fatalf("HTTP scenario failed: %v\n%s", err, output)
	}
}

// Record delivery attempts safely when a scenario submits concurrent requests.
type fakeMailSender struct {
	mu       sync.Mutex
	attempts []messaging.MailMessage
	err      error
}

func (f *fakeMailSender) Send(_ context.Context, message messaging.MailMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts = append(f.attempts, message)
	return f.err
}

type httpHarness struct {
	app     *server.ApplicationServer
	handler http.Handler
	mail    *fakeMailSender
	cookies http.CookieJar
}

func newHTTPHarness(t *testing.T) *httpHarness {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// Template and migration paths are relative to the repository root. Changing
	// directories here affects only this scenario's subprocess.
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	// Do not call GetConfig: local YAML files and environment settings must not
	// select a developer's database or mail service.
	cfg := &config.Config{
		App:      config.AppConfig{Name: "HTTP test application", WorkingDirectory: root, RenderNotifications: true},
		Database: config.DatabaseConfig{Driver: "sqlite3", Connection: filepath.Join(t.TempDir(), "application.db")},
		HTTP:     config.HTTPConfig{Hostname: "127.0.0.1", Port: 8080},
		Auth: config.AuthConfig{
			Enabled: true, JwtKey: "http-test-only-auth-signing-key",
			RegisterRequiresVerification: true,
			VerificationTokenExpiration:  time.Hour, ResetTokenExpiration: time.Hour,
			Services: map[string]map[string]string{"email": {"enabled": "true"}},
		},
	}
	cfg.HTTP.Session.JwtKey = "http-test-only-session-signing-key"
	cfg.HTTP.Session.TokenAge = time.Hour
	cfg.HTTP.Session.CookieAge = time.Hour
	cfg.HTTP.Session.Stores = map[string]map[string]string{"cookieStore": {"enabled": "true"}}
	cfg.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := session.ValidateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	db, err := database.Setup(cfg.Database.Driver, cfg.Database.Connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}

	mail := &fakeMailSender{}
	mailClient, err := messaging.NewMailClientWithSender("test@example.invalid", mail)
	if err != nil {
		t.Fatal(err)
	}
	cm := content.NewContentManager()
	cm.Configure(cfg)
	cm.HandleMessage, cm.HandleError, cm.HandleNotFound = util.HttpMessage, util.HttpError, util.HttpNotFound
	app := &server.ApplicationServer{
		Config: cfg, Database: db, Web: http.NewServeMux(),
		ContentManager: cm, SessionManager: session.NewSessionManager(cfg), Mail: mailClient,
	}
	if err := SetupHandlers(app); err != nil {
		t.Fatal(err)
	}
	if err := SetupAuthentication(app); err != nil {
		t.Fatal(err)
	}
	l, err := logger.NewZapLogger()
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &httpHarness{
		app: app, mail: mail, cookies: jar,
		handler: middleware.ChainMiddleware(app.Web, middleware.LoggerMiddleware(l), middleware.LoadSessionManagement(db, app.SessionManager)),
	}
}

// request preserves cookies without following redirects. Tests inspect the
// response before deciding which request comes next.
func (h *httpHarness) request(method, path string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(form.Encode()))
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if htmx {
		r.Header.Set("HX-Request", "true")
	}
	for _, cookie := range h.cookies.Cookies(r.URL) {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	response := w.Result()
	h.cookies.SetCookies(r.URL, response.Cookies())
	response.Body.Close()
	return w
}

func registrationForm() url.Values {
	return url.Values{
		"email":    {"person@example.invalid"},
		"password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"},
	}
}

func TestHTTPRegistration(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		if _, err := user.GetUserByEmail(h.app.Database, "person@example.invalid"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected an empty database, got %v", err)
		}
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/login" {
			t.Fatalf("registration response: status %d, headers %v, body %s", w.Code, w.Header(), w.Body.String())
		}
		u, err := user.GetUserByEmail(h.app.Database, "person@example.invalid")
		if err != nil {
			t.Fatal(err)
		}
		if u.Verified || !u.VerificationRequired || u.Password == "" || u.Password == "TestPassword1!" {
			t.Fatalf("unexpected persisted account state: verified=%v, verification required=%v, password stored correctly=%v", u.Verified, u.VerificationRequired, u.Password != "" && u.Password != "TestPassword1!")
		}
		if len(h.mail.attempts) != 1 {
			t.Fatalf("mail attempts = %d, want 1", len(h.mail.attempts))
		}
		message := h.mail.attempts[0]
		if message.To != u.Email || message.From != "test@example.invalid" || message.Subject != "Verify your account" || !strings.Contains(message.Body, "/auth/verify?token=") {
			t.Fatalf("unexpected verification email: %+v", message)
		}
		w = h.request(http.MethodGet, "/login", nil, false)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "You have been successfully registered.") {
			t.Fatalf("registration notification missing from next request: status %d, body %s", w.Code, w.Body.String())
		}
	})
}

func TestHTTPRegistrationMailFailure(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.mail.err = errors.New("test sender unavailable")
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		if w.Header().Get("HX-Redirect") != "" || !strings.Contains(w.Body.String(), "Your account was created, but verification email delivery failed.") {
			t.Fatalf("delivery failure not reported: status %d, headers %v, body %s", w.Code, w.Header(), w.Body.String())
		}
		if len(h.mail.attempts) != 1 {
			t.Fatalf("mail attempts = %d, want 1", len(h.mail.attempts))
		}
		if _, err := user.GetUserByEmail(h.app.Database, "person@example.invalid"); err != nil {
			t.Fatalf("account should remain available after delivery failure: %v", err)
		}
	})
}

func TestHTTPConcurrentRegistration(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		// Queue SQLite writes on one connection so this tests uniqueness, not
		// lock-timeout behavior. Schema initialization starts from an empty DB.
		h.app.Database.SetMaxOpenConns(1)
		passwords := []string{"FirstPassword1!", "SecondPassword2!"}
		for attempt := 0; attempt < 5; attempt++ {
			email := fmt.Sprintf("race-%d@example.invalid", attempt)
			responses := make([]*httptest.ResponseRecorder, len(passwords))
			start := make(chan struct{})
			var requests sync.WaitGroup
			for i, pw := range passwords {
				requests.Add(1)
				go func() {
					defer requests.Done()
					form := url.Values{"email": {email}, "password": {pw}, "passwordMatch": {pw}}
					r := httptest.NewRequest(http.MethodPost, "/auth/register/email", strings.NewReader(form.Encode()))
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					r.Header.Set("HX-Request", "true")
					w := httptest.NewRecorder()
					<-start
					h.handler.ServeHTTP(w, r)
					responses[i] = w
				}()
			}
			close(start)
			requests.Wait()

			winner, successes, duplicates := -1, 0, 0
			for i, w := range responses {
				if w.Code != http.StatusOK {
					t.Fatalf("registration status = %d, want 200", w.Code)
				}
				switch {
				case w.Header().Get("HX-Redirect") == "/login":
					winner = i
					successes++
				case strings.Contains(w.Body.String(), "The specified user is already registered"):
					duplicates++
				default:
					t.Fatalf("unexpected registration response: %s", w.Body.String())
				}
			}
			if successes != 1 || duplicates != 1 {
				t.Fatalf("concurrent registrations: successes=%d, duplicates=%d, want one of each", successes, duplicates)
			}
			u, err := user.GetUserByEmail(h.app.Database, email)
			if err != nil {
				t.Fatal(err)
			}
			if !password.CheckPasswordHash(passwords[winner], u.Password) || password.CheckPasswordHash(passwords[1-winner], u.Password) {
				t.Fatal("losing registration overwrote the winning password")
			}
			if len(h.mail.attempts) != attempt+1 {
				t.Fatalf("mail attempts = %d, want %d", len(h.mail.attempts), attempt+1)
			}
			h.assertRowCount(t, "user", attempt+1)
		}
	})
}

func TestHTTPRegistrationRejectsExistingAccount(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		form := registrationForm()
		form.Set("password", "DifferentPassword1!")
		form.Set("passwordMatch", "DifferentPassword1!")
		w := h.request(http.MethodPost, "/auth/register/email", form, true)
		assertFormRejected(t, w, "The specified user is already registered")
		h.assertUserUnchanged(t, u)
		h.assertRowCount(t, "user", 1)
	})
}

func TestHTTPVerificationUpdatesExistingAccount(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Fatal("could not register the test account")
		}
		u, err := user.GetUserByEmail(h.app.Database, registrationForm().Get("email"))
		if err != nil {
			t.Fatal(err)
		}
		token, err := user.NewVerificationToken(u.ID, []byte(h.app.Config.Auth.JwtKey), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		w = h.request(http.MethodGet, "/auth/verify?token="+url.QueryEscape(token), nil, true)
		if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("verification did not succeed")
		}
		updated, err := user.GetUserByID(h.app.Database, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !updated.Verified || updated.Password != u.Password || !updated.CreatedAt.Equal(u.CreatedAt) {
			t.Fatal("verification did not preserve the existing account")
		}
		h.assertRowCount(t, "user", 1)
	})
}

func TestHTTPRegistrationRejectsInvalidForm(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		// Initialize the schema so row counts detect writes to either table.
		if _, err := user.GetUserByEmail(h.app.Database, "person@example.invalid"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected an empty database, got %v", err)
		}
		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct {
				name, email, password, confirmation, message string
			}{
				{"mismatch", "person@example.invalid", "TestPassword1!", "DifferentPassword1!", "Passwords do not match."},
				{"weak password", "person@example.invalid", "short", "short", "Password values must contain at least 8 characters"},
				{"invalid email", "not-an-email", "TestPassword1!", "TestPassword1!", "Enter a valid email address."},
				{"missing email", "", "TestPassword1!", "TestPassword1!", "This field is required."},
				{"missing password", "person@example.invalid", "", "", "This field is required."},
				{"missing confirmation", "person@example.invalid", "TestPassword1!", "", "This field is required."},
			} {
				t.Run(fmt.Sprintf("%s/htmx=%v", tc.name, htmx), func(t *testing.T) {
					form := url.Values{"email": {tc.email}, "password": {tc.password}, "passwordMatch": {tc.confirmation}}
					w := h.request(http.MethodPost, "/auth/register/email", form, htmx)
					assertFormRejected(t, w, tc.message)
					h.assertRowCount(t, "user", 0)
					h.assertRowCount(t, "usertoken", 0)
					if len(h.mail.attempts) != 0 {
						t.Fatal("invalid registration attempted mail delivery")
					}
				})
			}
		}
	})
}

func assertFormRejected(t *testing.T, w *httptest.ResponseRecorder, message string) {
	t.Helper()
	if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "" || !strings.Contains(w.Body.String(), message) {
		t.Fatalf("expected field error %q: status %d, redirect %q, body %s", message, w.Code, w.Header().Get("HX-Redirect"), w.Body.String())
	}
}

func (h *httpHarness) assertRowCount(t *testing.T, table string, want int) {
	t.Helper()
	var count int
	if err := h.app.Database.Get(&count, "SELECT COUNT(*) FROM "+table); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Errorf("%s row count = %d, want %d", table, count, want)
	}
}

func (h *httpHarness) seedUser(t *testing.T, email string) *user.User {
	t.Helper()
	hash, err := password.HashPassword("TestPassword1!")
	if err != nil {
		t.Fatal(err)
	}
	u := &user.User{Email: email, Password: hash, Verified: true, RegistrationAuthType: "email"}
	if err := u.Create(context.Background(), h.app.Database); err != nil {
		t.Fatal(err)
	}
	u, err = user.GetUserByEmail(h.app.Database, email)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (h *httpHarness) assertUserUnchanged(t *testing.T, original *user.User) {
	t.Helper()
	got, err := user.GetUserByID(h.app.Database, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *original {
		t.Error("invalid form changed the stored user")
	}
	if len(h.mail.attempts) != 0 {
		t.Error("invalid form attempted mail delivery")
	}
}

func TestHTTPLoginAndResetRequestRejectInvalidEmail(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		// A legacy account with an invalid address proves validation stops both
		// authentication and reset-token creation before using the stored account.
		u := h.seedUser(t, "not-an-email")
		for _, path := range []string{"/auth/login/email", "/auth/reset/request/email"} {
			t.Run(path, func(t *testing.T) {
				w := h.request(http.MethodPost, path, url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
				assertFormRejected(t, w, "Enter a valid email address.")
				if !strings.Contains(w.Body.String(), `hx-post="`+path+`"`) {
					t.Error("validation response does not preserve the form submission URL")
				}
				h.assertUserUnchanged(t, u)
				h.assertRowCount(t, "usertoken", 0)
			})
		}
		w := h.request(http.MethodGet, "/auth/change/email", nil, true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Error("invalid login granted access to an authenticated route")
		}
	})
}

func TestHTTPResetRejectsInvalidPassword(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		token, err := user.NewAuthResetToken(u, []byte(h.app.Config.Auth.JwtKey), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := token.Create(h.app.Database); err != nil {
			t.Fatal(err)
		}
		path := "/auth/reset/email?token=" + url.QueryEscape(token.Token)
		for _, tc := range []struct{ password, confirmation, message string }{
			{"NewPassword1!", "DifferentPassword1!", "Passwords do not match."},
			{"short", "short", "Password values must contain at least 8 characters"},
			{"", "", "This field is required."},
		} {
			t.Run(tc.message, func(t *testing.T) {
				w := h.request(http.MethodPost, path, url.Values{"password": {tc.password}, "passwordMatch": {tc.confirmation}}, true)
				assertFormRejected(t, w, tc.message)
				if !strings.Contains(w.Body.String(), path) {
					t.Error("validation response lost the reset URL and token")
				}
				h.assertUserUnchanged(t, u)
				if _, err := user.GetAuthResetTokenByHash(h.app.Database, token.TokenHash); err != nil {
					t.Fatalf("invalid form consumed the reset token: %v", err)
				}
			})
		}
		w := h.request(http.MethodPost, path, url.Values{"password": {"NewPassword1!"}, "passwordMatch": {"NewPassword1!"}}, true)
		if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/login" {
			t.Fatal("corrected reset form did not succeed")
		}
		updated, err := user.GetUserByID(h.app.Database, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !password.CheckPasswordHash("NewPassword1!", updated.Password) {
			t.Error("corrected reset form did not update the password")
		}
		h.assertRowCount(t, "usertoken", 0)
	})
}

func TestHTTPChangeRejectsInvalidPassword(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "person@example.invalid")
		w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("could not log in the test user")
		}
		for _, tc := range []struct{ old, password, confirmation, message string }{
			{"TestPassword1!", "NewPassword1!", "DifferentPassword1!", "Passwords do not match."},
			{"TestPassword1!", "short", "short", "Password values must contain at least 8 characters"},
			{"TestPassword1!", "TestPassword1!", "TestPassword1!", "New passwords must be different from the previous password."},
			{"", "NewPassword1!", "NewPassword1!", "This field is required."},
		} {
			t.Run(tc.message, func(t *testing.T) {
				form := url.Values{"oldPassword": {tc.old}, "newPassword": {tc.password}, "newPasswordMatch": {tc.confirmation}}
				w := h.request(http.MethodPost, "/auth/change/email", form, true)
				assertFormRejected(t, w, tc.message)
				if !strings.Contains(w.Body.String(), `hx-post="/auth/change/email"`) {
					t.Error("validation response lost the change-password URL")
				}
				h.assertUserUnchanged(t, u)
			})
		}
		form := url.Values{"oldPassword": {"TestPassword1!"}, "newPassword": {"NewPassword1!"}, "newPasswordMatch": {"NewPassword1!"}}
		w = h.request(http.MethodPost, "/auth/change/email", form, true)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Your password has been changed.") {
			t.Fatal("corrected password-change form did not succeed")
		}
		updated, err := user.GetUserByID(h.app.Database, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !password.CheckPasswordHash("NewPassword1!", updated.Password) {
			t.Error("corrected password-change form did not update the password")
		}
	})
}

func TestHTTPContactRejectsInvalidForm(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		if err := database.RunMigrations(h.app.Database.DB, "modules/site/database/migrations"); err != nil {
			t.Fatal(err)
		}
		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct{ field, value, message string }{
				{"name", "", "This field is required."},
				{"message", "", "This field is required."},
				{"email", "not-an-email", "Enter a valid email address."},
			} {
				t.Run(fmt.Sprintf("%s/htmx=%v", tc.field, htmx), func(t *testing.T) {
					form := url.Values{"name": {"Test person"}, "email": {"person@example.invalid"}, "message": {"Test message"}}
					form.Set(tc.field, tc.value)
					w := h.request(http.MethodPost, "/contact", form, htmx)
					assertFormRejected(t, w, tc.message)
					if strings.Contains(w.Body.String(), "Your message has been submitted.") {
						t.Error("invalid contact form reported success")
					}
					h.assertRowCount(t, "hyperserver_contact_submission", 0)
					if len(h.mail.attempts) != 0 {
						t.Error("invalid contact form attempted mail delivery")
					}
				})
			}
		}
	})
}

func TestHTTPContact(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for i, htmx := range []bool{false, true} {
			form := url.Values{"name": {"Test person"}, "email": {"person@example.invalid"}, "message": {"Test message"}}
			w := h.request(http.MethodPost, "/contact", form, htmx)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Your message has been submitted.") {
				t.Fatalf("contact response (HTMX=%v): status %d, body %s", htmx, w.Code, w.Body.String())
			}
			if fullPage := strings.Contains(strings.ToLower(w.Body.String()), "<!doctype html>"); fullPage == htmx {
				t.Fatalf("contact response full page=%v, HTMX=%v", fullPage, htmx)
			}
			var count int
			if err := h.app.Database.Get(&count, "SELECT COUNT(*) FROM hyperserver_contact_submission WHERE email = ? AND message = ?", "person@example.invalid", "Test message"); err != nil {
				t.Fatal(err)
			}
			if count != i+1 {
				t.Fatalf("saved contact submissions = %d, want %d", count, i+1)
			}
		}
		if len(h.mail.attempts) != 0 {
			t.Fatalf("contact storage unexpectedly sent mail: %+v", h.mail.attempts)
		}
	})
}
