package application_test

import (
	"context"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth"
	email "github.com/trentnix/hyperserver/auth/modules/email"
	"github.com/trentnix/hyperserver/pkg/application"
)

func TestSharedRegistrationPage(t *testing.T) {
	cfg := authenticationConfig(t)
	cfg.HTTP.PublicOrigin = "http://localhost"
	cfg.Auth.Services["google"] = map[string]string{"enabled": "true", "clientID": "test-client", "clientSecret": "test-secret"}
	app, err := application.New(t.Context(), cfg, application.Options{Modules: authenticationModules})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	for _, hx := range []bool{false, true} {
		request := browserRequests(t, app, hx)
		status, header := http.StatusSeeOther, "Location"
		if hx {
			status, header = http.StatusOK, "HX-Redirect"
		}
		for _, path := range []string{"/auth/register", "/auth/register/email", "/auth/register/google"} {
			w := request("GET", path, nil)
			if w.Code != status || w.Header().Get(header) != "/register" || strings.Contains(w.Body.String(), "<form") || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("legacy registration %s: %d %v %s", path, w.Code, w.Header(), w.Body)
			}
			if w := request("POST", path, url.Values{"provider": {"email"}}); w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("legacy POST %s returned %d", path, w.Code)
			}
		}
		for _, method := range []string{"GET", "POST"} {
			const address = `invalid"><script>alert(1)</script>`
			const secret = "Do-not-redisplay-this-password"
			w := request(method, "/register", url.Values{"provider": {"email"}, "email": {address}, "password": {secret}, "passwordMatch": {secret}})
			body := w.Body.String()
			for _, want := range []string{`id="register"`, `action="/register"`, `name="provider" value="email"`, "Sign up with Google"} {
				if w.Code != http.StatusOK || !strings.Contains(body, want) {
					t.Fatalf("%s registration lost %q: %d %s", method, want, w.Code, body)
				}
			}
			if method == "POST" && (!strings.Contains(body, `id="email-error"`) || !strings.Contains(body, html.EscapeString(address))) {
				t.Fatal("validation lost the email or its error", body)
			}
			if strings.Contains(body, secret) || strings.Contains(body, "<script>alert") || strings.Count(body, `id="register"`) != 1 {
				t.Fatal("registration exposed input or duplicated the shared wrapper")
			}
		}
		if w := request("GET", "/register?provider=google", nil); w.Code != http.StatusNotFound {
			t.Fatalf("GET must not start OAuth: %d %s", w.Code, w.Body)
		}
		w := request("POST", "/register", url.Values{"provider": {"google"}})
		location, err := url.Parse(w.Header().Get(header))
		if err != nil || w.Code != status || location.Host != "accounts.google.com" || location.Query().Get("code_challenge_method") != "S256" {
			t.Fatalf("Google registration: %d %v %s", w.Code, w.Header(), w.Body)
		}
	}
}

type inlineRegistrationProvider struct{ customAuthProvider }

func (*inlineRegistrationProvider) AuthType() string { return "another-form" }
func (*inlineRegistrationProvider) GetRegisterButton() template.HTML {
	return `<a href="/register?provider=another-form">Another method</a>`
}
func (*inlineRegistrationProvider) RegistrationForm(*http.Request) (template.HTML, error) {
	return `<form id="another-form"></form>`, nil
}
func (p *inlineRegistrationProvider) Register(_ http.ResponseWriter, r *http.Request) auth.RegistrationResult {
	markup, _ := p.RegistrationForm(r)
	return auth.RegistrationResult{Form: markup}
}

func TestRegistrationProviderSelection(t *testing.T) {
	cfg := authenticationConfig(t)
	cfg.Auth.Services["another-form"] = map[string]string{"enabled": "true"}
	app, err := application.New(t.Context(), cfg, application.Options{
		Modules: authenticationModules,
		AuthProviders: func(context.Context, *application.Application) ([]auth.Descriptor, error) {
			return []auth.Descriptor{
				{Name: "another-form", New: func(auth.Dependencies) auth.AuthService { return &inlineRegistrationProvider{} }},
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
		w := request("GET", "/register", nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="another-form"`) || !strings.Contains(w.Body.String(), `href="/register?provider=email"`) {
			t.Fatalf("default form: %d %s", w.Code, w.Body)
		}
		for _, selected := range []string{"email", "another-form"} {
			for _, method := range []string{"GET", "POST"} {
				path := "/register?provider=" + selected
				if method == "POST" {
					path = "/register?provider=unknown" // Only the body selects a submission provider.
				}
				w := request(method, path, url.Values{"provider": {selected}})
				marker, alternate := `id="another-form"`, `href="/register?provider=email"`
				if selected == "email" {
					marker, alternate = `id="register-form"`, `href="/register?provider=another-form"`
				}
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), marker) || !strings.Contains(w.Body.String(), alternate) {
					t.Fatalf("%s %s lost the selected form or alternatives: %d %s", method, selected, w.Code, w.Body)
				}
			}
		}
		for _, method := range []string{"GET", "POST"} {
			for _, tc := range []struct {
				providers []string
				status    int
			}{
				{[]string{""}, 400}, {[]string{"email", "email"}, 400},
				{[]string{"email", "another-form"}, 400},
				{[]string{"unknown"}, 404}, {[]string{"google"}, 404},
			} {
				values := url.Values{"provider": tc.providers}
				path := "/register"
				if method == "GET" {
					path += "?" + values.Encode()
				}
				w := request(method, path, values)
				if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("%s %v: %d %s", method, tc.providers, w.Code, w.Body)
				}
			}
		}
		for _, path := range []string{"/register", "/register?provider=email"} {
			if w := request("POST", path, nil); w.Code != http.StatusBadRequest {
				t.Fatalf("missing provider accepted: %d %s", w.Code, w.Body)
			}
		}
	}
}
