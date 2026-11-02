package application_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAccountPage(t *testing.T) {
	for _, mode := range []string{"email", "google", "both"} {
		t.Run(mode, func(t *testing.T) {
			cfg := authenticationConfig(t)
			cfg.HTTP.PublicOrigin = "http://localhost"
			cfg.Auth.Services["google"] = map[string]string{"enabled": "false", "clientID": "test-client", "clientSecret": "test-secret"}
			if mode != "email" {
				cfg.Auth.Services["google"]["enabled"] = "true"
			}
			if mode == "google" {
				cfg.Auth.Services["email"]["enabled"] = "false"
			}
			app, err := application.New(t.Context(), cfg, application.Options{Modules: authenticationModules})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { app.Shutdown() })
			for _, state := range []string{"anonymous", "unverified", "verified", "passwordless"} {
				t.Run(state, func(t *testing.T) {
					var cookies []*http.Cookie
					if state != "anonymous" {
						u := &user.User{Email: state + "@example.com", RegistrationAuthType: "email", Password: "private-password-hash", VerificationRequired: true, Verified: state != "unverified"}
						if state == "passwordless" {
							u.RegistrationAuthType, u.Password = "google", ""
						}
						if err := app.Accounts.Create(t.Context(), u); err != nil {
							t.Fatal(err)
						}
						r := session.AddSessionManagerToRequestContext(httptest.NewRequest("GET", "http://example.test/", nil), app.Sessions)
						w := httptest.NewRecorder()
						if err := identity.SetAuthenticatedUser(w, r, u); err != nil {
							t.Fatal(err)
						}
						cookies = w.Result().Cookies()
					}
					for _, hx := range []bool{false, true} {
						r := httptest.NewRequest("GET", "http://example.test/auth/account", nil)
						if hx {
							r.Header.Set("HX-Request", "true")
						}
						for _, c := range cookies {
							r.AddCookie(c)
						}
						w := httptest.NewRecorder()
						app.Handler.ServeHTTP(w, r)
						if w.Header().Get("Cache-Control") != "no-store" {
							t.Fatal("account response can be cached")
						}
						body := w.Body.String()
						if strings.Contains(body, "private-password-hash") || strings.Contains(body, "test-secret") {
							t.Fatal("account response leaked credentials")
						}
						if state == "anonymous" {
							header := "Location"
							if hx {
								header = "HX-Redirect"
							}
							if w.Header().Get(header) != "/auth/login" {
								t.Fatalf("anonymous response: %d %s", w.Code, body)
							}
							continue
						}
						if state == "unverified" {
							if w.Code != http.StatusForbidden {
								t.Fatalf("unverified account: %d", w.Code)
							}
							continue
						}
						if w.Code != http.StatusOK || !strings.Contains(body, state+"@example.com") {
							t.Fatalf("account response: %d %s", w.Code, body)
						}
						if strings.Contains(body, `href="/auth/link/google"`) != (mode != "email") {
							t.Fatal("wrong linking option", body)
						}
						if strings.Contains(body, `href="/auth/change/email"`) != (mode != "google" && state != "passwordless") {
							t.Fatal("wrong password option", body)
						}
						if mode == "email" && state == "passwordless" && !strings.Contains(body, "No account controls") {
							t.Fatal("empty state missing")
						}
					}
				})
			}
		})
	}
}
