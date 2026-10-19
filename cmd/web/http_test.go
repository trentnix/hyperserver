package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	framework "github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// HTTP scenarios use subprocesses to isolate working directories and environment
// overrides. Each scenario owns its modules, providers, databases, and mail fake.
func runHTTPScenario(t *testing.T, scenario func(*httpHarness), configure ...func(*config.Config)) {
	t.Helper()
	if os.Getenv("HS_HTTP_TEST_CASE") == t.Name() {
		// These overrides would fail if the harness accidentally loaded runtime
		// configuration instead of using its explicit fixture.
		t.Setenv("HYPERSERVER_DATABASE_CONNECTION", filepath.Join(t.TempDir(), "missing", "developer.db"))
		t.Setenv("HYPERSERVER_AUTH_JWTKEY", "")
		t.Setenv("HYPERSERVER_HTTP_SESSION_JWTKEY", "")
		scenario(newHTTPHarness(t, configure...))
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
	// Go treats each slash-separated component as its own regular expression.
	// Anchor every component so a scenario named "mail" cannot also run "mail failure".
	parts := strings.Split(t.Name(), "/")
	for i, part := range parts {
		parts[i] = "^" + regexp.QuoteMeta(part) + "$"
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run="+strings.Join(parts, "/"))
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

func (f *fakeMailSender) Send(ctx context.Context, message messaging.MailMessage) error {
	f.mu.Lock()
	f.attempts = append(f.attempts, message)
	err := f.err
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return err
}

type capturedLogs struct {
	mu    sync.Mutex
	lines []string
}

func newTestLogger(logs *capturedLogs) *slog.Logger {
	return slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func (l *capturedLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, strings.TrimSuffix(string(p), "\n"))
	return len(p), nil
}

func (l *capturedLogs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

type httpHarness struct {
	baseURL string
	app     *application
	handler http.Handler
	mail    *fakeMailSender
	cookies http.CookieJar
	logs    *capturedLogs
}

func newHTTPHarness(t testing.TB, configure ...func(*config.Config)) *httpHarness {
	return newHTTPHarnessWithLogger(t, nil, configure...)
}

func newHTTPHarnessWithLogger(t testing.TB, log *slog.Logger, configure ...func(*config.Config)) *httpHarness {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// Do not call GetConfig: local YAML files and environment settings must not
	// select a developer's database or mail service.
	cfg := &config.Config{
		App:      config.AppConfig{Name: "HTTP test application", WorkingDirectory: root, RenderNotifications: true},
		Database: config.DatabaseConfig{Driver: "sqlite3", Connection: filepath.Join(t.TempDir(), "application.db")},
		HTTP:     config.HTTPConfig{ListenHost: "127.0.0.1", Port: 8080},
		Auth: config.AuthConfig{
			Enabled: true, JwtKey: "http-test-only-auth-signing-key0",
			RegistrationEnabled:          true,
			RegisterRequiresVerification: true,
			VerificationTokenExpiration:  time.Hour, ResetTokenExpiration: time.Hour,
			// Command tests do not need the password-reset response delay.
			ResetMinimumResponseTime: 0,
			Services:                 map[string]map[string]string{"email": {"enabled": "true"}},
		},
	}
	cfg.HTTP.Session.JwtKey = "http-test-only-session-signing-key"
	cfg.HTTP.Session.TokenAge = time.Hour
	cfg.HTTP.Session.CookieAge = time.Hour
	cfg.HTTP.Session.Stores = map[string]map[string]string{
		"cookieStore": {"enabled": "true"},
		"sqliteStore": {"enabled": "true", "connection": cfg.Database.Connection, "sessiontable": "session"},
	}
	cfg.HTTP.Session.Types = map[string]string{"default": "cookieStore", "auth-user-session": "sqliteStore"}
	for _, change := range configure {
		change(cfg)
	}

	mail := &fakeMailSender{}
	mailClient, err := messaging.NewMailClientWithSender("test@example.invalid", mail)
	if err != nil {
		t.Fatal(err)
	}
	logs := &capturedLogs{}
	if log == nil {
		log = newTestLogger(logs)
	}
	app, err := newApplication(logger.Set(t.Context(), log), *cfg, "", framework.Options{Services: framework.Services{Mail: mailClient}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &httpHarness{
		baseURL: "http://127.0.0.1:8080",
		app:     app, mail: mail, cookies: jar, logs: logs,
		handler: app.Handler,
	}
}

// request preserves cookies without following redirects. Tests inspect the
// response before deciding which request comes next.
func (h *httpHarness) request(method, path string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, h.baseURL+path, strings.NewReader(form.Encode()))
	r.Header.Set("Origin", h.baseURL)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	r = r.WithContext(ctx)
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

func TestHTTPHarnessSecureCookies(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			h := &httpHarness{baseURL: scheme + "://test.example", cookies: jar}
			h.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Scheme != scheme || (r.TLS != nil) != (scheme == "https") {
					t.Error("request URL and TLS state do not match the test origin")
				}
				if r.URL.Path == "/set" {
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "test", Path: "/", Secure: true})
					return
				}
				cookie, err := r.Cookie("session")
				if scheme == "https" {
					if err != nil || cookie.Value != "test" {
						t.Error("HTTPS request lost the secure session cookie")
					}
				} else if !errors.Is(err, http.ErrNoCookie) {
					t.Error("HTTP request sent a secure session cookie")
				}
			})
			h.request(http.MethodGet, "/set", nil, false)
			h.request(http.MethodGet, "/check", nil, false)
		})
	}
}

func (f *fakeMailSender) snapshot() []messaging.MailMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]messaging.MailMessage(nil), f.attempts...)
}

func (h *httpHarness) assertRowCount(t *testing.T, table string, want int) {
	t.Helper()
	var count int
	if err := h.app.contactDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Errorf("%s row count = %d, want %d", table, count, want)
	}
}

func (h *httpHarness) seedUser(t testing.TB, email string) *user.User {
	t.Helper()
	hash, err := password.HashPassword("TestPassword1!")
	if err != nil {
		t.Fatal(err)
	}
	u := &user.User{Email: email, Password: hash, Verified: true, RegistrationAuthType: "email"}
	if err := h.app.Accounts.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	u, err = h.app.Accounts.GetByEmail(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func registrationForm() url.Values {
	return url.Values{
		"email":    {"person@example.invalid"},
		"password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"},
	}
}

func (h *httpHarness) establishSession(t testing.TB, u *user.User) {
	t.Helper()
	r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.app.Sessions)
	w := httptest.NewRecorder()
	if err := identity.SetAuthenticatedUser(w, r, u); err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(h.baseURL)
	h.cookies.SetCookies(base, w.Result().Cookies())
}
