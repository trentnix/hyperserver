package web_test

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/auth/password"
	authweb "github.com/trentnix/hyperserver/auth/web"
	"github.com/trentnix/hyperserver/config"
	sqlitedb "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	sessionsqlite "github.com/trentnix/hyperserver/pkg/services/session/sqlite"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

// HTTP fixtures assemble public APIs rather than the reference command.
// Each instance owns its configuration, catalog, storage, cookies, and mail fake.
type httpHarness struct {
	baseURL    string
	app        *server.ApplicationServer
	db         *sql.DB
	accounts   user.AccountRepository
	sessions   *session.SessionManager
	mailClient *messaging.MailClient
	handler    http.Handler
	mail       *fakeMailSender
	logs       *capturedLogs
	cookies    http.CookieJar
}

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

func (h *httpHarness) seedUser(t testing.TB, email string) *user.User {
	t.Helper()
	hash, err := password.HashPassword("TestPassword1!")
	if err != nil {
		t.Fatal(err)
	}
	u := &user.User{Email: email, Password: hash, Verified: true, RegistrationAuthType: "email"}
	if err := h.accounts.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	u, err = h.accounts.GetByEmail(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (h *httpHarness) assertRowCount(t *testing.T, table string, want int) {
	t.Helper()
	var count int
	if err := h.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Errorf("%s row count = %d, want %d", table, count, want)
	}
}

func (h *httpHarness) assertUserUnchanged(t *testing.T, original *user.User) {
	t.Helper()
	got, err := h.accounts.GetByID(t.Context(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *original {
		t.Error("invalid form changed the stored user")
	}
	if len(h.mail.snapshot()) != 0 {
		t.Error("invalid form attempted mail delivery")
	}
}

func (h *httpHarness) assertNoTokenExposure(t *testing.T, w *httptest.ResponseRecorder, token string) {
	t.Helper()
	response := w.Body.String() + fmt.Sprint(w.Header())
	if strings.Contains(response, "token=") || (token != "" && strings.Contains(response, token)) {
		t.Error("response exposed an account token")
	}
	if token != "" && strings.Contains(h.logs.String(), token) {
		t.Error("logs exposed an account token")
	}
}

func assertFormRejected(t *testing.T, w *httptest.ResponseRecorder, message string) {
	t.Helper()
	if w.Code != http.StatusOK || w.Header().Get("HX-Redirect") != "" || !strings.Contains(w.Body.String(), message) {
		t.Fatalf("expected field error %q: status %d, redirect %q, body %s", message, w.Code, w.Header().Get("HX-Redirect"), w.Body.String())
	}
}

func mailLink(t *testing.T, message messaging.MailMessage, path string) *url.URL {
	t.Helper()
	match := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(message.Body)
	if len(match) != 2 {
		t.Fatal("email has no link")
	}
	link, err := url.Parse(html.UnescapeString(match[1]))
	if err != nil || link.Host != "127.0.0.1:8080" || link.Path != path || link.Query().Get("token") == "" {
		t.Fatal("email has an invalid account link")
	}
	return link
}

func assertRedirectResponse(t *testing.T, w *httptest.ResponseRecorder, destination string, htmx bool) {
	t.Helper()
	status, header, absent := http.StatusSeeOther, "Location", "HX-Redirect"
	if htmx {
		status, header, absent = http.StatusOK, "HX-Redirect", "Location"
	}
	if w.Code != status || w.Header().Get(header) != destination || w.Header().Get(absent) != "" || w.Body.Len() != 0 {
		t.Fatalf("redirect: status=%d headers=%v body=%q, want %d %s=%q", w.Code, w.Header(), w.Body.String(), status, header, destination)
	}
}

func runHTTPScenario(t *testing.T, scenario func(*httpHarness), configure ...func(*config.Config)) {
	t.Helper()
	scenario(newHTTPHarness(t, configure...))
}

func newHTTPHarness(t *testing.T, configure ...func(*config.Config)) *httpHarness {
	t.Helper()
	// Explicit configuration and temporary storage keep local files and environment
	// settings out of these tests. No working-directory changes or subprocesses.
	cfg := config.Config{
		App:  config.AppConfig{Name: "HTTP test application"},
		HTTP: config.HTTPConfig{ListenHost: "127.0.0.1", Port: 8080},
		Auth: config.AuthConfig{
			Enabled: true, JwtKey: "http-test-only-auth-signing-key0",
			RegistrationEnabled: true, RegisterRequiresVerification: true,
			VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
			ResetMinimumResponseTime: 0,
			Services:                 map[string]map[string]string{"email": {"enabled": "true"}},
		},
	}
	connection := filepath.Join(t.TempDir(), "accounts.db")
	cfg.HTTP.Session.JwtKey = "http-test-only-session-signing-key"
	cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
	cfg.HTTP.Session.Stores = map[string]map[string]string{
		"cookie": {"enabled": "true"},
		"sqlite": {"enabled": "true", "connection": connection, "sessiontable": "session"},
	}
	cfg.HTTP.Session.Types = map[string]string{"default": "cookie", "auth-user-session": "sqlite"}
	for _, change := range configure {
		change(&cfg)
	}
	app, err := server.NewApplicationServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})

	db, err := sqlitedb.Open(connection)
	if err != nil {
		t.Fatal(err)
	}
	app.OnShutdown("accounts", func(context.Context) error { return db.Close() })
	accounts := usersqlite.NewAccountRepository(db)
	if err := accounts.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	sessions, err := session.NewConfiguredManager(t.Context(), app.Config, map[string]session.ConfiguredProvider{
		"cookie": cookie.ConfiguredProvider(*app.Config),
		"sqlite": sessionsqlite.ConfiguredProvider(*app.Config),
	})
	if err != nil {
		t.Fatal(err)
	}
	app.OnShutdown("sessions", func(context.Context) error { return sessions.Close() })
	sender := &fakeMailSender{}
	mail, err := messaging.NewMailClientWithSender("test@example.invalid", sender)
	if err != nil {
		t.Fatal(err)
	}

	layouts := fstest.MapFS{
		"page.html":     {Data: []byte(`<!doctype html><html><body>{{renderPartial .PartialName .}}</body></html>`)},
		"fragment.html": {Data: []byte(`{{renderPartial .PartialName .}}`)},
	}
	app.ContentManager.RegisterLayouts(content.PageType, layouts, "page.html")
	app.ContentManager.RegisterLayouts(content.HtmxType, layouts, "fragment.html")
	web := authweb.Email{
		Accounts: accounts, Sessions: sessions, Mail: mail,
		LoginURL: "/auth/login/email", HomeURL: "/",
	}
	handler, err := web.Setup(t.Context(), app, []handlers.Descriptor{auth.Module(accounts, sessions, nil)})
	if err != nil {
		t.Fatal(err)
	}
	logs := &capturedLogs{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler, err = middleware.Web(handler, app.Config.HTTP, "", log)
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &httpHarness{baseURL: "http://127.0.0.1:8080", app: app, db: db, accounts: accounts, sessions: sessions, mailClient: mail, handler: handler, mail: sender, logs: logs, cookies: jar}
}

type fakeMailSender struct {
	mu                 sync.Mutex
	attempts           []messaging.MailMessage
	err                error
	includeBodyInError bool
	// Configure before starting requests. Timing tests can hold delivery here.
	beforeSend func(context.Context)
}

func (f *fakeMailSender) Send(ctx context.Context, message messaging.MailMessage) error {
	if f.beforeSend != nil {
		f.beforeSend(ctx)
	}
	f.mu.Lock()
	f.attempts = append(f.attempts, message)
	err, includeBody := f.err, f.includeBodyInError
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if includeBody {
		return fmt.Errorf("delivery failed for body: %s", message.Body)
	}
	return err
}

func (f *fakeMailSender) snapshot() []messaging.MailMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]messaging.MailMessage(nil), f.attempts...)
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
