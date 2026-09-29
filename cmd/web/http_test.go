package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
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
	mu                 sync.Mutex
	attempts           []messaging.MailMessage
	err                error
	includeBodyInError bool
}

func (f *fakeMailSender) Send(_ context.Context, message messaging.MailMessage) error {
	f.mu.Lock()
	f.attempts = append(f.attempts, message)
	err, includeBody := f.err, f.includeBodyInError
	f.mu.Unlock()
	if includeBody {
		return fmt.Errorf("delivery failed for body: %s", message.Body)
	}
	return err
}

type capturedLogs struct {
	mu    sync.Mutex
	lines []string
}

type testLogger struct {
	logs   *capturedLogs
	fields []logger.Field
}

func (l *testLogger) record(message string, fields ...logger.Field) {
	l.logs.mu.Lock()
	defer l.logs.mu.Unlock()
	l.logs.lines = append(l.logs.lines, fmt.Sprint(message, l.fields, fields))
}

func (l *testLogger) Debug(message string, fields ...logger.Field) { l.record(message, fields...) }
func (l *testLogger) Info(message string, fields ...logger.Field)  { l.record(message, fields...) }
func (l *testLogger) Warn(message string, fields ...logger.Field)  { l.record(message, fields...) }
func (l *testLogger) Error(message string, fields ...logger.Field) { l.record(message, fields...) }
func (l *testLogger) With(fields ...logger.Field) logger.Logger {
	return &testLogger{logs: l.logs, fields: append(append([]logger.Field(nil), l.fields...), fields...)}
}

func (l *capturedLogs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

type httpHarness struct {
	baseURL string
	app     *server.ApplicationServer
	handler http.Handler
	mail    *fakeMailSender
	cookies http.CookieJar
	logs    *capturedLogs
}

func newHTTPHarness(t *testing.T, configure ...func(*config.Config)) *httpHarness {
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
			RegistrationEnabled:          true,
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
	for _, change := range configure {
		change(cfg)
	}
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
	logs := &capturedLogs{}
	l := &testLogger{logs: logs}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &httpHarness{
		baseURL: "http://127.0.0.1:8080",
		app:     app, mail: mail, cookies: jar, logs: logs,
		handler: middleware.ChainMiddleware(app.Web, middleware.LoggerMiddleware(l), middleware.LoadSessionManagement(db, app.SessionManager)),
	}
}

// request preserves cookies without following redirects. Tests inspect the
// response before deciding which request comes next.
func (h *httpHarness) request(method, path string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, h.baseURL+path, strings.NewReader(form.Encode()))
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
		if len(h.mail.snapshot()) != 1 {
			t.Fatalf("mail attempts = %d, want 1", len(h.mail.snapshot()))
		}
		message := h.mail.snapshot()[0]
		if message.To != u.Email || message.From != "test@example.invalid" || message.Subject != "Verify your account" || !strings.Contains(message.Body, "/auth/verify?token=") {
			t.Fatalf("unexpected verification email: %+v", message)
		}
		w = h.request(http.MethodGet, "/login", nil, false)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "You have been successfully registered.") {
			t.Fatalf("registration notification missing from next request: status %d, body %s", w.Code, w.Body.String())
		}
	})
}

func TestHTTPRegistrationDisabled(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "existing@example.invalid")
		u.Verified, u.VerificationRequired = false, true
		if err := u.Update(context.Background(), h.app.Database); err != nil {
			t.Fatal(err)
		}
		u, err := user.GetUserByID(h.app.Database, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, htmx := range []bool{false, true} {
			for _, path := range []string{"/register", "/auth/register", "/auth/register/email"} {
				for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
					w := h.request(method, path, registrationForm(), htmx)
					if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), `id="register-form"`) {
						t.Errorf("disabled registration %s %s: status %d", method, path, w.Code)
					}
				}
			}
			for _, path := range []string{"/", "/login", "/auth/login", "/auth/login/email"} {
				w := h.request(http.MethodGet, path, nil, htmx)
				if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "/auth/register") || strings.Contains(w.Body.String(), `href="/register"`) {
					t.Errorf("page %s failed or offered disabled registration", path)
				}
			}
		}
		h.assertRowCount(t, "user", 1)
		h.assertUserUnchanged(t, u)

		h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {u.Email}}, true)
		if len(h.mail.snapshot()) != 1 {
			t.Fatal("disabling registration broke password recovery")
		}
		w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("disabling registration broke login")
		}
		h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if len(h.mail.snapshot()) != 2 {
			t.Fatal("disabling registration broke verification resend")
		}
	}, func(cfg *config.Config) { cfg.Auth.RegistrationEnabled = false })
}

func TestHTTPRegistrationWithoutVerification(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.mail.err = messaging.ErrMailUnavailable
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Fatal("registration without verification failed")
		}
		u, err := user.GetUserByEmail(h.app.Database, "person@example.invalid")
		if err != nil || u.NeedsVerification() || len(h.mail.snapshot()) != 0 {
			t.Fatal("optional verification blocked the account or attempted delivery")
		}
		w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("new account could not log in without verification")
		}
	}, func(cfg *config.Config) { cfg.Auth.RegisterRequiresVerification = false })
}

func TestHTTPRegistrationCanRecoverFromMailFailure(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.mail.err = errors.New("delivery temporarily unavailable")
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		assertFormRejected(t, w, "Your account was created, but verification email delivery failed.")
		if len(h.mail.snapshot()) != 1 {
			t.Fatal("registration did not attempt delivery")
		}
		link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		h.assertNoTokenExposure(t, w, link.Query().Get("token"))
		w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("pending account could not log in to request verification")
		}
		h.mail.err = nil
		w = h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if len(h.mail.snapshot()) != 2 {
			t.Fatal("verification resend did not deliver instructions")
		}
		link = mailLink(t, h.mail.snapshot()[1], "/auth/verify")
		h.assertNoTokenExposure(t, w, link.Query().Get("token"))
		h.request(http.MethodGet, link.RequestURI(), nil, false)
		u, err := user.GetUserByEmail(h.app.Database, "person@example.invalid")
		if err != nil || !u.Verified {
			t.Fatal("account did not recover after successful resend")
		}
		h.assertRowCount(t, "user", 1)
	})
}

func TestHTTPRegistrationMailFailure(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for i, tc := range []struct {
			name string
			err  error
		}{
			{"delivery failure", errors.New("test delivery failed")},
			{"unavailable", messaging.ErrMailUnavailable},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h.mail.err = tc.err
				form := registrationForm()
				form.Set("email", fmt.Sprintf("mail-failure-%d@example.invalid", i))
				w := h.request(http.MethodPost, "/auth/register/email", form, true)
				assertFormRejected(t, w, "Your account was created, but verification email delivery failed.")
				if len(h.mail.snapshot()) != i+1 {
					t.Fatalf("mail attempts = %d, want %d", len(h.mail.snapshot()), i+1)
				}
				u, err := user.GetUserByEmail(h.app.Database, form.Get("email"))
				if err != nil {
					t.Fatalf("account should remain available after delivery failure: %v", err)
				}
				if u.Verified || !u.VerificationRequired {
					t.Error("delivery failure changed the account's verification requirements")
				}
				w = h.request(http.MethodGet, "/login", nil, false)
				if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "You have been successfully registered.") {
					t.Error("delivery failure produced a success notification or broke the next request")
				}
			})
		}
	})
}

func TestHTTPTestEmailDeliveryOutcome(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		path := "/test-email?to=person@example.invalid"
		h.mail.err = messaging.ErrMailUnavailable
		w := h.request(http.MethodPost, path, nil, true)
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "failed to send test email") || strings.Contains(w.Body.String(), "test email sent to") {
			t.Fatalf("unavailable mail response: status %d, body %s", w.Code, w.Body.String())
		}
		h.mail.err = nil
		w = h.request(http.MethodPost, path, nil, true)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "test email sent to person@example.invalid") {
			t.Fatalf("accepted mail response: status %d, body %s", w.Code, w.Body.String())
		}
		if len(h.mail.snapshot()) != 2 {
			t.Fatalf("mail attempts = %d, want 2", len(h.mail.snapshot()))
		}
	})
}

func TestHTTPMailEscapesDynamicHTML(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		input := `<img src=x onerror="alert(1)"> & test`
		h.app.Config.App.Name = input
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		if w.Header().Get("HX-Redirect") != "/login" || len(h.mail.snapshot()) != 1 {
			t.Fatal("registration failed")
		}
		body := h.mail.snapshot()[0].Body
		if strings.Contains(body, "<img") || !strings.Contains(body, "&lt;img") || !strings.Contains(body, "&amp; test") {
			t.Errorf("application name was not escaped: %s", body)
		}
		if !strings.Contains(body, `<a href="http://127.0.0.1:8080/auth/verify?token=`) {
			t.Error("verification link did not remain usable")
		}
		w = h.request(http.MethodPost, "/test-email?to=person@example.invalid&body="+url.QueryEscape(input), nil, true)
		if w.Code != http.StatusOK || len(h.mail.snapshot()) != 2 {
			t.Fatal("test email failed")
		}
		body = h.mail.snapshot()[1].Body
		if strings.Contains(body, "<img") || !strings.Contains(body, "&lt;img") {
			t.Errorf("custom test-email text was not escaped: %s", body)
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
			if len(h.mail.snapshot()) != attempt+1 {
				t.Fatalf("mail attempts = %d, want %d", len(h.mail.snapshot()), attempt+1)
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

func TestHTTPAccountLinkOrigins(t *testing.T) {
	for _, tc := range []struct {
		name, origin, wantScheme, wantHost string
		secure                             bool
		port                               uint16
	}{
		{name: "direct HTTP", port: 80, wantScheme: "http", wantHost: "127.0.0.1"},
		{name: "direct HTTPS", secure: true, port: 443, wantScheme: "https", wantHost: "127.0.0.1"},
		{name: "HTTPS proxy", origin: "https://accounts.example", port: 8080, wantScheme: "https", wantHost: "accounts.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				if tc.secure {
					h.baseURL = "https://127.0.0.1"
				}
				next := h.handler
				h.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					r.Host = "attacker.example"
					r.Header.Set("X-Forwarded-Proto", "https://attacker.example/?leak=")
					r.Header.Set("X-Forwarded-Host", "attacker.example")
					r.Header.Set("Forwarded", "host=attacker.example;proto=http")
					next.ServeHTTP(w, r)
				})
				checkLink := func(index int, path string) *url.URL {
					t.Helper()
					messages := h.mail.snapshot()
					if len(messages) != index+1 {
						t.Fatalf("mail attempts = %d, want %d", len(messages), index+1)
					}
					match := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(messages[index].Body)
					if len(match) != 2 {
						t.Fatal("email has no link")
					}
					link, err := url.Parse(html.UnescapeString(match[1]))
					if err != nil || link.Scheme != tc.wantScheme || link.Host != tc.wantHost || link.Path != path || link.Query().Get("token") == "" {
						t.Fatalf("unexpected account link %v, error %v", link, err)
					}
					return link
				}

				h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
				checkLink(0, "/auth/verify")
				h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {"person@example.invalid"}}, false)
				resetLink := checkLink(1, "/auth/reset/email")
				checkResetAction := func(w *httptest.ResponseRecorder) {
					t.Helper()
					for _, attribute := range []string{"action", "hx-post"} {
						if !strings.Contains(w.Body.String(), attribute+`="`+html.EscapeString(resetLink.RequestURI())+`"`) {
							t.Errorf("%s must use a relative reset URL with the token", attribute)
						}
					}
				}
				for _, htmx := range []bool{false, true} {
					w := h.request(http.MethodGet, resetLink.RequestURI(), nil, htmx)
					checkResetAction(w)
					w = h.request(http.MethodPost, resetLink.RequestURI(), url.Values{
						"password": {"NewPassword1!"}, "passwordMatch": {"DifferentPassword1!"},
					}, htmx)
					if !strings.Contains(w.Body.String(), "Passwords do not match.") {
						t.Fatal("invalid reset did not redisplay the form")
					}
					checkResetAction(w)
				}
				h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {"person@example.invalid"}, "password": {"TestPassword1!"}}, true)
				h.request(http.MethodPost, "/auth/request/verify", nil, true)
				checkLink(2, "/auth/verify")
			}, func(cfg *config.Config) {
				cfg.HTTP.Port = tc.port
				cfg.HTTP.PublicOrigin = tc.origin
			})
		})
	}
}

func TestHTTPResetRequestEmailsInstructions(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.app.Config.App.Name = `<img src=x> & test`
		var nativeAction, nativeUserID string
		for i, htmx := range []bool{false, true} {
			u := h.seedUser(t, fmt.Sprintf("reset%d@example.invalid", i))
			form := url.Values{"email": {u.Email}, "to": {"attacker@example.invalid"}}
			w := h.request(http.MethodPost, "/auth/reset/request/email", form, htmx)

			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Password reset request received.") {
				t.Error("reset request did not return a generic acknowledgment")
			}
			if len(h.mail.snapshot()) != i+1 {
				t.Fatal("reset instructions were not emailed")
			}
			message := h.mail.snapshot()[i]
			if message.To != u.Email || message.Subject != "Reset your password" || strings.Contains(message.Body, "<img") || !strings.Contains(message.Body, "&lt;img") {
				t.Error("reset email used the wrong recipient, subject, or escaping")
			}
			link := mailLink(t, message, "/auth/reset/email")
			token := link.Query().Get("token")
			h.assertNoTokenExposure(t, w, token)
			unknown := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {"unknown@example.invalid"}}, htmx)
			if unknown.Code != w.Code || unknown.Body.String() != w.Body.String() || len(h.mail.snapshot()) != i+1 {
				t.Error("unknown account changed the acknowledgment or caused mail delivery")
			}
			resetUser, err := user.ValidateResetToken(h.app.Database, token)
			if err != nil || resetUser.ID != u.ID {
				t.Fatal("emailed reset token does not belong to the account")
			}
			// Following the emailed link must not put its token in request logs.
			reset := h.request(http.MethodGet, link.RequestURI(), nil, htmx)
			if reset.Code != http.StatusOK || !strings.Contains(reset.Body.String(), "reset-password-form") {
				t.Error("emailed reset link did not open the reset form")
			}
			if !htmx {
				formTag := regexp.MustCompile(`<form id="reset-password-form"[^>]*>`).FindString(reset.Body.String())
				action := regexp.MustCompile(`\saction="([^"]+)"`).FindStringSubmatch(formTag)
				if !strings.Contains(formTag, `method="post"`) || len(action) != 2 {
					t.Fatal("reset form must submit passwords by POST without JavaScript")
				}
				actionURL, err := url.Parse(html.UnescapeString(action[1]))
				if err != nil || actionURL.Query().Get("token") != token {
					t.Fatal("reset form action lost the token")
				}
				nativeAction, nativeUserID = actionURL.RequestURI(), u.ID
			}
			if strings.Contains(h.logs.String(), token) {
				t.Error("following the reset link exposed its token in logs")
			}
		}
		h.request(http.MethodPost, nativeAction, url.Values{
			"password": {"ReplacementPassword1!"}, "passwordMatch": {"ReplacementPassword1!"},
		}, false)
		stored, err := user.GetUserByID(h.app.Database, nativeUserID)
		if err != nil || !password.CheckPasswordHash("ReplacementPassword1!", stored.Password) {
			t.Fatal("ordinary reset form submission did not update the password")
		}
	})
}

func TestHTTPVerificationResendEmailsInstructions(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		u := h.seedUser(t, "verify@example.invalid")
		u.Verified, u.VerificationRequired = false, true
		if err := u.Update(context.Background(), h.app.Database); err != nil {
			t.Fatal(err)
		}
		w := h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("login failed")
		}
		for i, htmx := range []bool{false, true} {
			w := h.request(http.MethodPost, "/auth/request/verify", url.Values{"email": {"attacker@example.invalid"}}, htmx)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "check your email") {
				t.Error("verification request did not return an acknowledgment")
			}
			if len(h.mail.snapshot()) != i+1 {
				t.Fatal("verification instructions were not emailed")
			}
			message := h.mail.snapshot()[i]
			if message.To != u.Email || message.Subject != "Verify your account" {
				t.Error("verification email used the wrong recipient or subject")
			}
			link := mailLink(t, message, "/auth/verify")
			h.assertNoTokenExposure(t, w, link.Query().Get("token"))
		}
		link := mailLink(t, h.mail.snapshot()[0], "/auth/verify")
		w = h.request(http.MethodGet, link.RequestURI(), nil, true)
		h.assertNoTokenExposure(t, w, link.Query().Get("token"))
		verified, err := user.GetUserByID(h.app.Database, u.ID)
		if err != nil || !verified.Verified {
			t.Fatal("emailed link did not verify the account")
		}
	})
}

func TestHTTPResetMailFailureAcknowledgment(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		for _, htmx := range []bool{false, true} {
			for i, tc := range []struct {
				name        string
				err         error
				includeBody bool
			}{
				{name: "disabled delivery", err: messaging.ErrMailUnavailable},
				{name: "timeout", err: context.DeadlineExceeded},
				{name: "error includes message body", includeBody: true},
			} {
				t.Run(fmt.Sprintf("%s/htmx=%t", tc.name, htmx), func(t *testing.T) {
					u := h.seedUser(t, fmt.Sprintf("failure-%d-%t@example.invalid", i, htmx))
					h.mail.err, h.mail.includeBodyInError = tc.err, tc.includeBody
					before := len(h.mail.snapshot())
					w := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {u.Email}}, htmx)
					if len(h.mail.snapshot()) != before+1 {
						t.Fatal("reset delivery was not attempted")
					}
					unknown := h.request(http.MethodPost, "/auth/reset/request/email", url.Values{"email": {"unknown@example.invalid"}}, htmx)
					if w.Code != http.StatusOK || unknown.Code != w.Code || unknown.Body.String() != w.Body.String() {
						t.Fatal("mail failure changed the acknowledgment")
					}
					link := mailLink(t, h.mail.snapshot()[before], "/auth/reset/email")
					h.assertNoTokenExposure(t, w, link.Query().Get("token"))
					if !strings.Contains(h.logs.String(), "password reset instructions could not be sent") {
						t.Fatal("delivery failure was not logged")
					}
					stored, err := user.GetUserByID(h.app.Database, u.ID)
					if err != nil || *stored != *u {
						t.Fatal("failed reset request changed the account")
					}
				})
			}
		}
	})
}

func TestHTTPVerificationResendAuthorizationAndFailure(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		// The reference renderer currently returns 200 for error pages. Check the
		// status supplied by auth separately without changing the renderer here.
		var errorStatus int
		renderError := h.app.ContentManager.HandleError
		h.app.ContentManager.HandleError = func(w http.ResponseWriter, r *http.Request, message string, err error, status int) {
			errorStatus = status
			renderError(w, r, message, err, status)
		}
		w := h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if errorStatus != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Authentication required") || len(h.mail.snapshot()) != 0 {
			t.Fatal("anonymous resend was not denied")
		}
		u := h.seedUser(t, "resend@example.invalid")
		u.Verified, u.VerificationRequired = false, true
		if err := u.Update(context.Background(), h.app.Database); err != nil {
			t.Fatal(err)
		}
		w = h.request(http.MethodPost, "/auth/login/email", url.Values{"email": {u.Email}, "password": {"TestPassword1!"}}, true)
		if w.Header().Get("HX-Redirect") != "/" {
			t.Fatal("login failed")
		}
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w = h.request(method, "/auth/request/verify", nil, true)
			if (w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound) || len(h.mail.snapshot()) != 0 {
				t.Error("non-POST resend was not rejected without delivery")
			}
		}
		for _, htmx := range []bool{false, true} {
			for _, tc := range []struct {
				err         error
				includeBody bool
			}{
				{messaging.ErrMailUnavailable, false}, {context.DeadlineExceeded, false}, {nil, true},
			} {
				h.mail.err, h.mail.includeBodyInError = tc.err, tc.includeBody
				before := len(h.mail.snapshot())
				w = h.request(http.MethodPost, "/auth/request/verify", nil, htmx)
				if errorStatus != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "Unable to send verification instructions.") || len(h.mail.snapshot()) != before+1 {
					t.Fatal("verification delivery failure was not reported")
				}
				link := mailLink(t, h.mail.snapshot()[before], "/auth/verify")
				h.assertNoTokenExposure(t, w, link.Query().Get("token"))
				if !strings.Contains(h.logs.String(), "verification email delivery failed") {
					t.Error("verification failure was not logged")
				}
			}
		}
		stored, err := user.GetUserByID(h.app.Database, u.ID)
		if err != nil || stored.Verified {
			t.Fatal("resend failure verified the account")
		}
		before := len(h.mail.snapshot())
		u.Verified = true
		if err := u.Update(context.Background(), h.app.Database); err != nil {
			t.Fatal(err)
		}
		w = h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if w.Code != http.StatusOK || len(h.mail.snapshot()) != before {
			t.Error("verified account caused another email")
		}
		u.Verified, u.RegistrationAuthType = false, "missing-provider"
		if err := u.Update(context.Background(), h.app.Database); err != nil {
			t.Fatal(err)
		}
		w = h.request(http.MethodPost, "/auth/request/verify", nil, true)
		if errorStatus != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "Unable to send verification instructions.") || len(h.mail.snapshot()) != before {
			t.Error("missing verification provider was not handled")
		}
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
					if len(h.mail.snapshot()) != 0 {
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
	if len(h.mail.snapshot()) != 0 {
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
					if len(h.mail.snapshot()) != 0 {
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
		if len(h.mail.snapshot()) != 0 {
			t.Fatalf("contact storage unexpectedly sent mail: %+v", h.mail.snapshot())
		}
	})
}
