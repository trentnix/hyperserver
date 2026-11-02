package application_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/application"
)

func TestConfiguredLoginChoices(t *testing.T) {
	for _, mode := range []string{"email", "google", "both"} {
		t.Run(mode, func(t *testing.T) {
			cfg := authenticationConfig(t)
			cfg.HTTP.PublicOrigin = "http://localhost"
			cfg.Auth.Services = map[string]map[string]string{
				"email":  {"enabled": "false"},
				"google": {"enabled": "false", "clientID": "configured-client", "clientSecret": "configured-secret"},
			}
			if mode != "google" {
				cfg.Auth.Services["email"]["enabled"] = "true"
			}
			if mode != "email" {
				cfg.Auth.Services["google"]["enabled"] = "true"
			}
			app, err := application.New(t.Context(), cfg, application.Options{Modules: authenticationModules})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { app.Shutdown() })
			for _, htmx := range []bool{false, true} {
				request := browserRequests(t, app, htmx)
				w := request("GET", "/auth/login", nil)
				if w.Code != 200 {
					t.Fatalf("login: %d %s", w.Code, w.Body)
				}
				body := w.Body.String()
				if strings.Contains(body, `id="login-form"`) != (mode != "google") {
					t.Fatal("incorrect email form", body)
				}
				if strings.Contains(body, "Sign in with Google") != (mode != "email") {
					t.Fatal("incorrect Google option", body)
				}
				if strings.Contains(body, "configured-secret") || strings.Contains(body, `hx-trigger="load"`) {
					t.Fatal("credentials exposed or login depends on JavaScript")
				}
				if mode == "both" {
					w = request("POST", "/auth/login/email", nil)
					if !strings.Contains(w.Body.String(), "Sign in with Google") {
						t.Fatal("validation removed alternative provider")
					}
				}
				if mode == "email" {
					for _, path := range []string{"/auth/login/google", "/auth/callback/google", "/auth/link/google", "/auth/google/icon.png"} {
						if w := request("GET", path, nil); w.Code != 404 {
							t.Fatalf("disabled Google route %s returned %d", path, w.Code)
						}
					}
				}
				if mode == "google" {
					if w := request("GET", "/auth/login/email", nil); w.Code != 404 {
						t.Fatal("disabled email remains reachable", w.Code)
					}
				}
				w = request("GET", "/auth/register", nil)
				if w.Code != 200 || strings.Contains(w.Body.String(), `id="register-form"`) != (mode != "google") ||
					strings.Contains(w.Body.String(), `action="/auth/register/google"`) != (mode != "email") {
					t.Fatalf("incorrect registration forms: %d %s", w.Code, w.Body)
				}
				if mode != "google" {
					w = request("GET", "/auth/register/email", nil)
					if strings.Contains(w.Body.String(), `action="/auth/register/google"`) != (mode == "both") {
						t.Fatal("direct registration lost provider alternatives", w.Body)
					}
					w = request("POST", "/auth/register/email", url.Values{"email": {"invalid"}, "password": {"short"}, "passwordMatch": {"different"}})
					if !strings.Contains(w.Body.String(), `id="register-form"`) ||
						strings.Contains(w.Body.String(), `action="/auth/register/google"`) != (mode == "both") ||
						strings.Contains(w.Body.String(), `value="short"`) {
						t.Fatal("registration validation lost alternatives or exposed the password", w.Body)
					}
				}
			}
		})
	}
}
