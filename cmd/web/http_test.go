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
	"testing"
	"time"

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

// Requests and mail delivery are synchronous in this harness.
type fakeMailSender struct {
	attempts []messaging.MailMessage
	err      error
}

func (f *fakeMailSender) Send(_ context.Context, message messaging.MailMessage) error {
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

func TestHTTPRegistrationRejectsInvalidForm(t *testing.T) {
	t.Skip("Known defect: registration ignores field validation errors. Enable this regression test when the validation fix is implemented.")
	runHTTPScenario(t, func(h *httpHarness) {
		form := registrationForm()
		form.Set("passwordMatch", "DifferentPassword1!")
		w := h.request(http.MethodPost, "/auth/register/email", form, true)
		if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "" || !strings.Contains(w.Body.String(), "input-error") {
			t.Fatalf("invalid form response: status %d, headers %v, body %s", w.Code, w.Header(), w.Body.String())
		}
		if len(h.mail.attempts) != 0 {
			t.Fatalf("invalid registration attempted mail delivery: %+v", h.mail.attempts)
		}
		if _, err := user.GetUserByEmail(h.app.Database, form.Get("email")); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("invalid registration created an account or lookup failed: %v", err)
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
