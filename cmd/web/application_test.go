package main

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

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// blogConfig loads the shipped templates in an isolated installation. Mail is
// disabled so the command's wiring can be exercised without an SMTP server.
func blogConfig(t *testing.T) config.Config {
	t.Helper()
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "HYPERSERVER_") {
			t.Setenv(key, value)
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
	directory := t.TempDir()
	for _, name := range []string{"config-template.yaml", "modules/blog/config-template.yaml"} {
		data, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, strings.ReplaceAll(name, "config-template.yaml", "config.yaml"))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		contents := strings.NewReplacer(
			"REPLACE_WITH_RANDOM_SESSION_KEY", strings.Repeat("s", 64),
			"REPLACE_WITH_DIFFERENT_RANDOM_AUTH_KEY", strings.Repeat("a", 64),
		).Replace(string(data))
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(directory)
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Auth.RegisterRequiresVerification = false
	cfg.Mail = config.MailConfig{}
	return cfg
}

// TestDefaultApplication verifies the command's actual composition, rather than
// a test-only selection of modules, rendering, and authentication providers.
func TestDefaultApplication(t *testing.T) {
	cfg := blogConfig(t)
	app, err := newApplication(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ShutdownContext(context.Background()); err != nil {
			t.Error(err)
		}
	})
	cookies := make(map[string]*http.Cookie)
	request := func(method, path string, values url.Values, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, cfg.HTTP.PublicOrigin+path, strings.NewReader(values.Encode()))
		if values != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		app.Handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: status=%d, want=%d, body=%s", method, path, w.Code, status, w.Body)
		}
		for _, cookie := range w.Result().Cookies() {
			if cookie.MaxAge < 0 {
				delete(cookies, cookie.Name)
			} else {
				cookies[cookie.Name] = cookie
			}
		}
		return w
	}

	w := request("GET", "/", nil, http.StatusSeeOther)
	if w.Header().Get("Location") != "/auth/register" {
		t.Fatal("fresh installation did not open registration", w.Header())
	}
	w = request("GET", "/auth/register/email", nil, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Set up your blog") {
		t.Fatal("registration did not use the blog setup layout")
	}
	request("GET", "/assets/css/blog.css", nil, http.StatusOK)
	credentials := url.Values{
		"provider": {"email"}, "email": {"other@example.com"}, "password": {"Sample-Password-123!"}, "passwordMatch": {"Sample-Password-123!"},
	}
	request("POST", "/auth/register/email", credentials, http.StatusForbidden)
	if account, err := app.Accounts.GetByEmail(t.Context(), "other@example.com"); !errors.Is(err, user.ErrUserNotFound) || account != nil {
		t.Fatalf("rejected address lookup: account=%v err=%v", account, err)
	}
	credentials.Set("email", "admin@example.com")
	request("POST", "/auth/register/email", credentials, http.StatusSeeOther)
	w = request("GET", "/", nil, http.StatusOK)
	for _, want := range []string{"My Blog", "No posts yet."} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("fresh homepage missing %q", want)
		}
	}
	for _, private := range []string{"Trent Nix", "trentnix.com", "trentnix@gmail.com"} {
		if strings.Contains(w.Body.String(), private) {
			t.Errorf("fresh homepage exposes %q", private)
		}
	}

	request("POST", "/login", credentials, http.StatusSeeOther)
	w = request("GET", "/auth/continue", nil, http.StatusSeeOther)
	if w.Header().Get("Location") != "/profile" {
		t.Fatal("first login did not open Site Profile", w.Header())
	}
	w = request("GET", "/profile", nil, http.StatusOK)
	if !strings.Contains(w.Body.String(), `id="profile-form"`) {
		t.Fatal("Site Profile editor was not rendered")
	}
	// Check after setup and login so authorization cannot mask demo routes.
	request("GET", "/contact", nil, http.StatusNotFound)
	for _, path := range []string{"/contact", "/test-email", "/session-example"} {
		request("POST", path, url.Values{}, http.StatusNotFound)
	}
}

// TestDefaultApplicationRequiresBlogConfiguration prevents an old reference-site
// configuration from silently starting a blog without an administrator policy.
func TestDefaultApplicationRequiresBlogConfiguration(t *testing.T) {
	cfg := blogConfig(t)
	if err := os.Remove(filepath.Join(cfg.App.WorkingDirectory, "modules/blog/config.yaml")); err != nil {
		t.Fatal(err)
	}
	app, err := newApplication(t.Context(), cfg)
	if app != nil {
		_ = app.ShutdownContext(context.Background())
		t.Fatal("started without blog configuration")
	}
	if err == nil || !strings.Contains(err.Error(), "blog.adminEmail") {
		t.Fatalf("missing administrator policy: %v", err)
	}
}
