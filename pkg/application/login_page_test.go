package application_test

import (
	"context"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth"
	email "github.com/trentnix/hyperserver/auth/modules/email"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestSharedLoginPage(t *testing.T) {
	cfg := authenticationConfig(t)
	cfg.HTTP.PublicOrigin = "http://localhost"
	cfg.Auth.Services["google"] = map[string]string{"enabled": "true", "clientID": "test-client", "clientSecret": "test-secret"}
	app, err := application.New(t.Context(), cfg, application.Options{Modules: authenticationModules})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	for _, hx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", hx), func(t *testing.T) {
			request := browserRequests(t, app, hx)
			status, header := http.StatusSeeOther, "Location"
			if hx {
				status, header = http.StatusOK, "HX-Redirect"
			}
			for _, path := range []string{"/auth/login", "/auth/login/email", "/auth/login/google"} {
				w := request("GET", path, nil)
				if w.Code != status || w.Header().Get(header) != "/login" || strings.Contains(w.Body.String(), "<form") || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("legacy login %s: %d %v %s", path, w.Code, w.Header(), w.Body)
				}
				if w := request("POST", path, url.Values{"provider": {"email"}}); w.Code != http.StatusMethodNotAllowed {
					t.Fatalf("legacy POST %s returned %d", path, w.Code)
				}
			}
			const email = `invalid"><script>alert(1)</script>`
			const secret = "Do-not-redisplay-this-password"
			w := request("POST", "/login", url.Values{"provider": {"email"}, "email": {email}, "password": {secret}})
			body := w.Body.String()
			for _, want := range []string{`id="login"`, `action="/login"`, `name="provider" value="email"`, `value="` + html.EscapeString(email) + `"`, `id="email-error"`, "Sign in with Google"} {
				if w.Code != http.StatusOK || !strings.Contains(body, want) {
					t.Fatalf("validation lost %q: %d %s", want, w.Code, body)
				}
			}
			if strings.Contains(body, secret) || strings.Contains(body, "<script>alert") || strings.Count(body, `id="login"`) != 1 {
				t.Fatal("validation exposed input or duplicated the shared wrapper")
			}
			if w := request("GET", "/login?provider=google", nil); w.Code != http.StatusNotFound {
				t.Fatalf("GET must not select or start a redirect provider: %d %s", w.Code, w.Body)
			}
			w = request("POST", "/login", url.Values{"provider": {"google"}})
			location, err := url.Parse(w.Header().Get(header))
			if err != nil || w.Code != status || location.Host != "accounts.google.com" || location.Query().Get("code_challenge_method") != "S256" {
				t.Fatalf("Google start: %d %v %s", w.Code, w.Header(), w.Body)
			}
		})
	}
}

type inlineLoginProvider struct{ customAuthProvider }

func (*inlineLoginProvider) AuthType() string { return "another-form" }
func (*inlineLoginProvider) GetLoginButton() template.HTML {
	return `<a href="/login?provider=another-form">Another method</a>`
}
func (*inlineLoginProvider) LoginForm(*http.Request) (template.HTML, error) {
	return `<form id="another-form"></form>`, nil
}
func (p *inlineLoginProvider) Login(_ http.ResponseWriter, r *http.Request) auth.LoginResult {
	markup, _ := p.LoginForm(r)
	return auth.LoginResult{Form: markup}
}

func TestInlineLoginProviderSelection(t *testing.T) {
	for _, passwordAvailable := range []bool{true, false} {
		cfg := authenticationConfig(t)
		cfg.Auth.Services["another-form"] = map[string]string{"enabled": "true"}
		app, err := application.New(t.Context(), cfg, application.Options{
			Modules: authenticationModules,
			AuthPolicy: auth.Policy{LoginAccount: func(context.Context) (*user.User, error) {
				account := &user.User{}
				if passwordAvailable {
					account.Password = "configured"
				}
				return account, nil
			}},
			AuthProviders: func(context.Context, *application.Application) ([]auth.Descriptor, error) {
				return []auth.Descriptor{
					{Name: "another-form", New: func(auth.Dependencies) auth.AuthService { return &inlineLoginProvider{} }},
					email.Provider(),
				}, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { app.Shutdown() })
		for _, hx := range []bool{false, true} {
			request := browserRequests(t, app, hx)
			w := request("GET", "/login", nil)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="another-form"`) {
				t.Fatalf("default form: %d %s", w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), `href="/login?provider=email"`) != passwordAvailable {
				t.Fatalf("email choice does not match availability: %s", w.Body)
			}
			w = request("GET", "/login?provider=email", nil)
			if !passwordAvailable {
				if w.Code != http.StatusNotFound {
					t.Fatalf("unavailable form: %d %s", w.Code, w.Body)
				}
				continue
			}
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `name="email"`) || !strings.Contains(w.Body.String(), `href="/login?provider=another-form"`) || strings.Contains(w.Body.String(), `id="another-form"`) {
				t.Fatalf("selected email form: %d %s", w.Code, w.Body)
			}
			w = request("GET", "/login?provider=another-form", nil)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="another-form"`) || strings.Contains(w.Body.String(), `name="email"`) {
				t.Fatalf("switched form: %d %s", w.Code, w.Body)
			}
			for _, selected := range []string{"email", "another-form"} {
				w = request("POST", "/login?provider=unknown", url.Values{"provider": {selected}})
				marker := `id="another-form"`
				if selected == "email" {
					marker = `id="email-error"`
				}
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), marker) {
					t.Fatalf("%s validation lost selected form: %d %s", selected, w.Code, w.Body)
				}
			}
			for _, tc := range []struct {
				query  string
				status int
			}{
				{"?provider=", 400},
				{"?provider=email&provider=another-form", 400},
				{"?provider=email&provider=email", 400},
				{"?provider=unknown", 404},
				{"?provider=google", 404},
			} {
				w = request("GET", "/login"+tc.query, nil)
				if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("%s: %d %s", tc.query, w.Code, w.Body)
				}
			}
		}
	}
}

func TestLoginRequiresExplicitProvider(t *testing.T) {
	app, err := application.New(t.Context(), authenticationConfig(t), application.Options{Modules: authenticationModules})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	for _, hx := range []bool{false, true} {
		request := browserRequests(t, app, hx)
		for _, tc := range []struct {
			name, query string
			providers   []string
			status      int
		}{
			{"missing", "", nil, 400},
			{"empty", "", []string{""}, 400},
			{"query only", "?provider=email", nil, 400},
			{"duplicate", "", []string{"email", "email"}, 400},
			{"ambiguous", "", []string{"email", "google"}, 400},
			{"unknown", "", []string{"unknown"}, 404},
			{"disabled", "", []string{"google"}, 404},
		} {
			w := request("POST", "/login"+tc.query, url.Values{"provider": tc.providers})
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s HTMX=%t: %d %s", tc.name, hx, w.Code, w.Body)
			}
		}
	}
}
