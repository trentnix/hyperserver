package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

const testEmail = "reader@example.com"
const testPassword = "Example-password1!"

func testSettings(t *testing.T, provider, path string) settings {
	t.Helper()
	return settings{
		notesStore: provider, notesPath: path,
		accountsPath: filepath.Join(t.TempDir(), "accounts.db"),
		sessionKey:   strings.Repeat("s", 32), authKey: strings.Repeat("a", 32),
	}
}

func TestAuthenticationWorkflow(t *testing.T) {
	for _, hx := range []bool{false, true} {
		name := "navigation"
		if hx {
			name = "HTMX"
		}
		t.Run(name, func(t *testing.T) {
			s := testSettings(t, "files", filepath.Join(t.TempDir(), "notes"))
			app, err := newApplication(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := app.Shutdown(); err != nil {
					t.Error(err)
				}
			})
			if err := createUser(t.Context(), app.accounts, testEmail, testPassword); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(newHTTPServer(app).Handler)
			t.Cleanup(srv.Close)
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client := srv.Client()
			client.Jar = jar
			client.Timeout = 5 * time.Second
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			request := func(method, path, body, origin string) (*http.Response, string) {
				t.Helper()
				r, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				if method == "POST" {
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				if origin != "" {
					r.Header.Set("Origin", origin)
				}
				if hx {
					r.Header.Set("HX-Request", "true")
				}
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				data, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				return response, string(data)
			}
			redirect := func(response *http.Response, destination string) {
				t.Helper()
				status, header := 303, "Location"
				if hx {
					status, header = 200, "HX-Redirect"
				}
				if response.StatusCode != status || response.Header.Get(header) != destination || response.Header.Get("Cache-Control") != "no-store" {
					t.Fatalf("redirect = %d %v, want %s", response.StatusCode, response.Header, destination)
				}
			}

			response, body := request("GET", "/notes", "", "")
			if response.StatusCode != 200 || !strings.Contains(body, "Log in") || strings.Contains(body, `action="/notes"`) {
				t.Fatalf("anonymous page = %d %s", response.StatusCode, body)
			}
			response, _ = request("POST", "/notes", "text=must-not-save", srv.URL)
			redirect(response, "/auth/login/email")
			response, body = request("GET", "/auth/login/email", "", "")
			if response.StatusCode != 200 || !strings.Contains(body, `action="/auth/login/email"`) || strings.Contains(body, "<!doctype html>") == hx {
				t.Fatalf("login form = %d %s", response.StatusCode, body)
			}
			for _, email := range []string{testEmail, "missing@example.com"} {
				response, body = request("POST", "/auth/login/email", url.Values{"email": {email}, "password": {"wrong"}}.Encode(), srv.URL)
				if response.StatusCode != 200 || !strings.Contains(body, "Invalid login/password") {
					t.Fatalf("failed login = %d %s", response.StatusCode, body)
				}
				for _, c := range response.Cookies() {
					if c.Name == "auth-user-session" && c.MaxAge >= 0 {
						t.Fatal("failed login issued identity cookie")
					}
				}
			}
			credentials := url.Values{"email": {strings.ToUpper(testEmail)}, "password": {testPassword}}.Encode()
			response, _ = request("POST", "/auth/login/email", credentials, "https://attacker.test")
			if response.StatusCode != 403 {
				t.Fatal("cross-origin login accepted")
			}
			response, _ = request("POST", "/auth/login/email", credentials, srv.URL)
			redirect(response, "/notes")
			var identity *http.Cookie
			for _, c := range response.Cookies() {
				if c.Name == "auth-user-session" {
					identity = c
					if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Value == "" {
						t.Fatalf("unsafe identity cookie: %+v", c)
					}
				}
			}
			if identity == nil {
				t.Fatal("missing identity cookie")
			}
			response, body = request("GET", "/notes", "", "")
			if response.StatusCode != 200 || !strings.Contains(body, `action="/notes"`) || strings.Contains(body, "must-not-save") {
				t.Fatalf("authenticated page = %d %s", response.StatusCode, body)
			}
			if (!hx && !strings.Contains(body, "Log out")) || !strings.Contains(body, "successfully logged in") {
				t.Fatal("missing logout or notification", body)
			}
			response, _ = request("POST", "/notes", "text=authenticated-note", srv.URL)
			redirect(response, "/notes")

			// Reopen the same account and session storage, retaining the signed cookie.
			srv.Close()
			if err := app.Shutdown(); err != nil {
				t.Fatal(err)
			}
			app, err = newApplication(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			srv = httptest.NewServer(newHTTPServer(app).Handler)
			t.Cleanup(srv.Close)
			response, body = request("GET", "/notes", "", "")
			if response.StatusCode != 200 || !strings.Contains(body, "authenticated-note") || !strings.Contains(body, `action="/notes"`) {
				t.Fatalf("restart lost account, note or session: %d %s", response.StatusCode, body)
			}
			response, _ = request("POST", "/auth/logout", "", "https://attacker.test")
			if response.StatusCode != 403 {
				t.Fatal("cross-origin logout accepted")
			}
			response, _ = request("GET", "/auth/logout", "", "")
			if response.StatusCode != 405 {
				t.Fatal("GET allowed to log out")
			}
			response, _ = request("POST", "/auth/logout", "", srv.URL)
			redirect(response, "/notes")
			response, _ = request("POST", "/notes", "text=logged-out", srv.URL)
			redirect(response, "/auth/login/email")
			base, _ := url.Parse(srv.URL)
			jar.SetCookies(base, []*http.Cookie{identity})
			response, _ = request("POST", "/notes", "text=replayed-cookie", srv.URL)
			redirect(response, "/auth/login/email")
			identity.Value = "invalid-token"
			jar.SetCookies(base, []*http.Cookie{identity})
			response, _ = request("POST", "/notes", "text=tampered-cookie", srv.URL)
			if response.StatusCode != 400 {
				t.Fatalf("tampered cookie = %d", response.StatusCode)
			}
			jar.SetCookies(base, []*http.Cookie{{Name: "auth-user-session", Path: "/", MaxAge: -1}})
			_, body = request("GET", "/notes", "", "")
			if strings.Count(body, "<li>") != 1 {
				t.Fatal("unauthorized request changed notes", body)
			}
			response, _ = request("POST", "/auth/register/email", credentials, srv.URL)
			if response.StatusCode != 403 {
				t.Fatalf("registration enabled: %d", response.StatusCode)
			}
		})
	}
}

func TestAccountProvisioning(t *testing.T) {
	s := testSettings(t, "files", filepath.Join(t.TempDir(), "notes"))
	app, err := newApplication(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	for _, tc := range []struct{ email, secret string }{{"bad", testPassword}, {testEmail, "weak"}} {
		if err := createUser(t.Context(), app.accounts, tc.email, tc.secret); err == nil {
			t.Fatal("accepted invalid account")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := createUser(ctx, app.accounts, testEmail, testPassword); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := app.accounts.GetByEmail(t.Context(), testEmail); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatal("failed provisioning created an account", err)
	}
	if err := createUser(t.Context(), app.accounts, testEmail, testPassword); err != nil {
		t.Fatal(err)
	}
	if err := createUser(t.Context(), app.accounts, strings.ToUpper(testEmail), "Changed-password2!"); !errors.Is(err, user.ErrEmailAlreadyExists) {
		t.Fatal(err)
	}
	u, err := app.accounts.GetByEmail(t.Context(), testEmail)
	if err != nil || u.Password == testPassword || !password.CheckPasswordHash(testPassword, u.Password) || u.NeedsVerification() {
		t.Fatal("provisioning replaced credentials or stored plaintext", err)
	}
}

func TestAuthenticationStartupErrors(t *testing.T) {
	for _, change := range []func(*settings){
		func(s *settings) { s.sessionKey = "" },
		func(s *settings) { s.authKey = "short" },
		func(s *settings) { s.authKey = s.sessionKey },
		func(s *settings) { s.accountsPath = "" },
		func(s *settings) { s.accountsPath = filepath.Join(t.TempDir(), "missing", "accounts.db") },
	} {
		s := testSettings(t, "files", filepath.Join(t.TempDir(), "notes"))
		change(&s)
		app, err := newApplication(t.Context(), s)
		if err == nil || app != nil {
			t.Fatal("accepted invalid configuration")
		}
		if _, err := os.Stat(s.notesPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed auth startup created notes storage")
		}
	}
}

func TestSessionFailureBlocksNoteWrite(t *testing.T) {
	s := testSettings(t, "files", filepath.Join(t.TempDir(), "notes"))
	app, err := newApplication(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	handler := loggedInHandler(t, app)
	if err := app.sessions.Close(); err != nil {
		t.Fatal(err)
	}
	w := noteRequest(t, handler, "POST", "/notes", "text=must-not-save", false, false)
	if w.Code != 500 || strings.Contains(w.Body.String(), "database") {
		t.Fatalf("session failure = %d %s", w.Code, w.Body)
	}
	entries, err := os.ReadDir(s.notesPath)
	if err != nil || len(entries) != 0 {
		t.Fatal("storage failure allowed a write", entries, err)
	}
}

type unavailableAccounts struct{ user.AccountRepository }

func (unavailableAccounts) GetByID(context.Context, string) (*user.User, error) {
	return nil, errors.New("private-account-storage-detail")
}

func TestAccountFailureBlocksNoteWrite(t *testing.T) {
	s := testSettings(t, "files", filepath.Join(t.TempDir(), "notes"))
	app, err := newApplication(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	if err := createUser(t.Context(), app.accounts, testEmail, testPassword); err != nil {
		t.Fatal(err)
	}
	login := noteRequest(t, newHTTPServer(app).Handler, "POST", "/auth/login/email", "email="+testEmail+"&password="+testPassword, false, false)
	if login.Code != 303 {
		t.Fatal("login failed", login.Body)
	}
	app.accounts = unavailableAccounts{app.accounts}
	handler := newHTTPServer(app).Handler
	request := httptest.NewRequest("POST", "http://example.test/notes", strings.NewReader("text=must-not-save"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range login.Result().Cookies() {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 500 || strings.Contains(response.Body.String(), "private-account-storage-detail") {
		t.Fatalf("account failure = %d %s", response.Code, response.Body)
	}
	if entries, err := os.ReadDir(s.notesPath); err != nil || len(entries) != 0 {
		t.Fatal("account failure allowed a write", entries, err)
	}
}

func loggedInHandler(t *testing.T, app *application) http.Handler {
	t.Helper()
	if err := createUser(t.Context(), app.accounts, testEmail, testPassword); err != nil {
		t.Fatal(err)
	}
	handler := newHTTPServer(app).Handler
	w := noteRequest(t, handler, "POST", "/auth/login/email", "email="+testEmail+"&password="+testPassword, false, false)
	if w.Code != 303 {
		t.Fatalf("login = %d %s", w.Code, w.Body)
	}
	var identity *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "auth-user-session" {
			identity = cookie
		}
	}
	if identity == nil {
		t.Fatal("login did not issue an identity cookie")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(identity)
		handler.ServeHTTP(w, r)
	})
}
