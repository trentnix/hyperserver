package application_test

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// The test provider is registered like an imported module. No application factory
// selects it, and only tests explicitly enabling its name can reach its routes.
func init() {
	auth.Register(auth.Descriptor{Name: "policy-test", New: func(d auth.Dependencies) auth.AuthService {
		return &policyTestProvider{dependencies: d}
	}})
}

type policyTestProvider struct{ dependencies auth.Dependencies }

func (*policyTestProvider) AuthType() string { return "policy-test" }
func (p *policyTestProvider) Init(*server.ApplicationServer) error {
	if p.dependencies.Accounts == nil || p.dependencies.Sessions == nil || p.dependencies.Policy.AllowAccount == nil {
		return errors.New("provider did not receive application services and policy")
	}
	return nil
}
func (*policyTestProvider) GetLoginButton() template.HTML { return "Test login" }
func (p *policyTestProvider) Login(w http.ResponseWriter, r *http.Request) auth.LoginResult {
	return auth.LoginResult{Account: p.Callback(w, r)}
}
func (p *policyTestProvider) Callback(w http.ResponseWriter, r *http.Request) *user.User {
	u, err := p.dependencies.Accounts.GetByEmail(r.Context(), "reader@example.com")
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return nil
	}
	if r.URL.Query().Get("invalid") == "true" {
		u.SessionVersion = 0
	}
	return u
}

func TestRegisteredProviderUsesSharedPolicy(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		for _, callback := range []bool{false, true} {
			for _, outcome := range []string{"allowed", "denied", "unavailable", "session failure"} {
				t.Run(outcome+map[bool]string{false: "/login", true: "/callback"}[callback]+map[bool]string{false: "/ordinary", true: "/HTMX"}[htmx], func(t *testing.T) {
					cfg := authenticationConfig(t)
					cfg.Auth.Services = map[string]map[string]string{"policy-test": {"enabled": "true"}}
					cfg.Auth.RegistrationEnabled = false
					admissions, logins := 0, 0
					app, err := application.New(t.Context(), cfg, application.Options{Modules: authenticationModules, AuthPolicy: auth.Policy{
						AllowAccount: func(ctx context.Context, email string) (bool, error) {
							admissions++
							if ctx.Err() != nil || email != "reader@example.com" {
								t.Errorf("wrong admission input: %q %v", email, ctx.Err())
							}
							if outcome == "unavailable" {
								return false, errors.New("private policy failure")
							}
							return outcome != "denied", nil
						},
						AfterLogin: func(w http.ResponseWriter, r *http.Request, u *user.User) {
							logins++
							if user.GetUserFromContext(r.Context()) != u {
								t.Error("login hook has no authenticated identity")
							}
							http.SetCookie(w, &http.Cookie{Name: "policy-login", Value: "yes"})
						},
					}})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { app.Shutdown() })
					u := &user.User{Email: "reader@example.com", Verified: true}
					if err := app.Accounts.Create(t.Context(), u); err != nil {
						t.Fatal(err)
					}
					method, path := "POST", "/login"
					if callback {
						method, path = "GET", "/auth/callback/policy-test"
					}
					if outcome == "session failure" {
						path += "?invalid=true"
					}
					w := browserRequests(t, app, htmx)(method, path, url.Values{"provider": {"policy-test"}})
					want := map[string]int{"allowed": http.StatusSeeOther, "denied": http.StatusForbidden, "unavailable": http.StatusServiceUnavailable, "session failure": http.StatusInternalServerError}[outcome]
					if htmx && outcome == "allowed" {
						want = http.StatusOK
					}
					if w.Code != want || admissions != 1 {
						t.Fatalf("status=%d admissions=%d body=%s", w.Code, admissions, w.Body)
					}
					if (logins == 1) != (outcome == "allowed") {
						t.Fatalf("login callbacks=%d", logins)
					}
					found := false
					for _, c := range w.Result().Cookies() {
						if c.Name == "policy-login" {
							found = true
						}
					}
					if found != (outcome == "allowed") || strings.Contains(w.Body.String(), "private policy failure") {
						t.Fatal("failed login leaked success or private error")
					}
				})
			}
		}
	}
}

func TestEmailRegistrationUsesSharedPolicy(t *testing.T) {
	for _, outcome := range []string{"allowed", "denied", "unavailable", "closed"} {
		t.Run(outcome, func(t *testing.T) {
			cfg := authenticationConfig(t)
			cfg.Auth.RegisterRequiresVerification = false
			app, err := application.New(t.Context(), cfg, application.Options{Modules: authenticationModules, AuthPolicy: auth.Policy{
				AllowAccount: func(context.Context, string) (bool, error) {
					if outcome == "unavailable" {
						return false, errors.New("private policy failure")
					}
					return outcome != "denied", nil
				},
				BeforeRegister: func(w http.ResponseWriter, _ *http.Request) bool {
					if outcome == "closed" {
						w.WriteHeader(http.StatusGone)
						return false
					}
					return true
				},
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { app.Shutdown() })
			request := browserRequests(t, app, false)
			if outcome == "closed" {
				for _, path := range []string{"/register", "/register?provider=email"} {
					if w := request("GET", path, nil); w.Code != http.StatusGone {
						t.Fatalf("closed form returned %d", w.Code)
					}
				}
			}
			w := request("POST", "/register", url.Values{"provider": {"email"}, "email": {"reader@example.com"}, "password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"}})
			want := map[string]int{"allowed": http.StatusSeeOther, "denied": http.StatusForbidden, "unavailable": http.StatusServiceUnavailable, "closed": http.StatusGone}[outcome]
			if w.Code != want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			_, err = app.Accounts.GetByEmail(t.Context(), "reader@example.com")
			if outcome == "allowed" && err != nil || outcome != "allowed" && !errors.Is(err, user.ErrUserNotFound) {
				t.Fatalf("unexpected stored account: %v", err)
			}
		})
	}
}

// Keep the test provider's response behavior explicit for manager-owned sessions.
var _ auth.LoginCallbackHandler = (*policyTestProvider)(nil)
