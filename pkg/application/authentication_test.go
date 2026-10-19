package application_test

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trentnix/hyperserver/auth"
	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservices "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func authenticationConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := storageConfig(t)
	cfg.Auth.AccountStorage.Provider = "sqlite"
	cfg.Auth.AccountStorage.Options = map[string]string{"connection": "accounts.db"}
	cfg.Auth.Services = map[string]map[string]string{"email": {"enabled": "true"}}
	cfg.Auth.RegistrationEnabled = true
	cfg.HTTP.PublicOrigin = "http://example.test"
	cfg.HTTP.Session.JwtKey = strings.Repeat("s", 64)
	cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
	cfg.HTTP.Session.Types = map[string]string{"default": "sqlitestore"}
	cfg.HTTP.Session.Stores = map[string]map[string]string{
		"sqlitestore": {"enabled": "true", "connection": "sessions.db", "sessiontable": "sessions"},
	}
	return cfg
}

func authenticationModules(_ context.Context, app *application.Application) ([]handlers.Descriptor, error) {
	layouts := fstest.MapFS{"page.html": {Data: []byte(`{{renderPartial .PartialName .}}`)}}
	app.ContentManager.RegisterLayouts(contentservices.PageType, layouts, "page.html")
	app.ContentManager.RegisterLayouts(contentservices.HtmxType, layouts, "page.html")
	return []handlers.Descriptor{auth.Module(nil, nil, nil)}, nil
}

// Requests use the returned handler directly, without adding auth middleware.
func browserRequests(t *testing.T, app *application.Application, htmx bool) func(string, string, url.Values) *httptest.ResponseRecorder {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return func(method, path string, fields url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(fields.Encode()))
		if method == http.MethodPost {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if htmx {
			r.Header.Set("HX-Request", "true")
		}
		for _, cookie := range jar.Cookies(r.URL) {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		app.Handler.ServeHTTP(w, r)
		jar.SetCookies(r.URL, w.Result().Cookies())
		return w
	}
}

func TestConfiguredAuthenticationHTTPFlow(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "HTMX"}[htmx], func(t *testing.T) {
			cfg := authenticationConfig(t)
			var catalog []handlers.Descriptor
			app, err := application.New(t.Context(), cfg, application.Options{Modules: func(ctx context.Context, app *application.Application) ([]handlers.Descriptor, error) {
				catalog, _ = authenticationModules(ctx, app)
				feature := &moduleStub{routes: func(routes *routing.Routes) error {
					routes.HandleFunc("GET /public", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "public") })
					guard := authmiddleware.RequireAuthentication(app.Accounts, app.ContentManager)
					routes.Handle("GET /private", guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if session.GetSessionManager(r) != app.Sessions {
							t.Error("wrong session manager")
						}
						u := user.GetUserFromContext(r.Context())
						if u == nil {
							t.Error("identity was not loaded into request context")
							return
						}
						fmt.Fprint(w, u.Email)
					})))
					return routes.Err()
				}}
				catalog = append(catalog, handlers.Descriptor{Name: "feature", New: func() handlers.Handler { return feature }})
				return catalog, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { app.Shutdown() })
			request := browserRequests(t, app, htmx)
			redirectStatus, redirectHeader := http.StatusSeeOther, "Location"
			if htmx {
				redirectStatus, redirectHeader = http.StatusOK, "HX-Redirect"
			}
			assertRedirect := func(w *httptest.ResponseRecorder, target string) {
				t.Helper()
				if w.Code != redirectStatus || w.Header().Get(redirectHeader) != target {
					t.Fatalf("redirect = %d %v %s, want %s", w.Code, w.Header(), w.Body, target)
				}
			}
			if w := request(http.MethodGet, "/public", nil); w.Code != http.StatusOK || w.Body.String() != "public" {
				t.Fatal("public route requires authentication", w.Code, w.Body)
			}
			assertRedirect(request(http.MethodGet, "/private", nil), "/auth/login")
			if w := request(http.MethodGet, "/auth/login/email", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `action="/auth/login/email"`) {
				t.Fatal("login form unavailable", w.Code, w.Body)
			}
			fields := url.Values{"email": {"reader@example.com"}, "password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"}}
			assertRedirect(request(http.MethodPost, "/auth/register/email", fields), "/auth/login")
			if _, err := app.Accounts.GetByEmail(t.Context(), fields.Get("email")); err != nil {
				t.Fatal("registration did not use configured accounts", err)
			}
			bad := url.Values{"email": {fields.Get("email")}, "password": {"wrong"}}
			if w := request(http.MethodPost, "/auth/login/email", bad); w.Header().Get(redirectHeader) != "" || !strings.Contains(w.Body.String(), "Invalid login/password") {
				t.Fatal("invalid credentials were accepted", w.Code, w.Header())
			}
			assertRedirect(request(http.MethodPost, "/auth/login/email", fields), "/private")
			if w := request(http.MethodGet, "/private", nil); w.Code != http.StatusOK || w.Body.String() != fields.Get("email") {
				t.Fatal("authenticated request failed", w.Code, w.Body)
			}
			request(http.MethodPost, "/auth/logout", nil)
			assertRedirect(request(http.MethodGet, "/private", nil), "/auth/login")

			r := httptest.NewRequest(http.MethodGet, "http://example.test/public", nil)
			r.AddCookie(&http.Cookie{Name: "auth-user-session", Value: "invalid"})
			w := httptest.NewRecorder()
			app.Handler.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest || w.Body.String() == "public" {
				t.Fatal("invalid identity cookie reached handler", w.Code, w.Body)
			}
		})
	}
}

type customAuthProvider struct {
	initialized bool
	initErr     error
}

type verificationSender struct{ messages []messaging.MailMessage }

func (s *verificationSender) Send(_ context.Context, message messaging.MailMessage) error {
	s.messages = append(s.messages, message)
	return nil
}

func TestConfiguredVerificationUsesSuppliedMail(t *testing.T) {
	cfg := authenticationConfig(t)
	cfg.Auth.RegisterRequiresVerification = true
	sender := &verificationSender{}
	mail, err := messaging.NewMailClientWithSender("sender@example.com", sender)
	if err != nil {
		t.Fatal(err)
	}
	app, err := application.New(t.Context(), cfg, application.Options{
		Services: application.Services{Mail: mail}, Modules: authenticationModules,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	if len(sender.messages) != 0 {
		t.Fatal("startup attempted delivery")
	}
	w := browserRequests(t, app, false)(http.MethodPost, "/auth/register/email", url.Values{
		"email": {"reader@example.com"}, "password": {"TestPassword1!"}, "passwordMatch": {"TestPassword1!"},
	})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/auth/login" || len(sender.messages) != 1 {
		t.Fatalf("registration status=%d deliveries=%d body=%s", w.Code, len(sender.messages), w.Body)
	}
	message := sender.messages[0]
	if message.To != "reader@example.com" || !strings.Contains(message.Body, "http://example.test/auth/verify?") {
		t.Fatal("verification message did not use account address and public origin")
	}
	u, err := app.Accounts.GetByEmail(t.Context(), "reader@example.com")
	if err != nil || !u.NeedsVerification() {
		t.Fatalf("verification state: user=%v err=%v", u, err)
	}
}

func (p *customAuthProvider) Init(*server.ApplicationServer) error {
	p.initialized = true
	return p.initErr
}
func (*customAuthProvider) AuthType() string              { return "custom" }
func (*customAuthProvider) GetLoginButton() template.HTML { return "custom" }
func (p *customAuthProvider) GetLogin(w http.ResponseWriter, r *http.Request) {
	if !p.initialized {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (*customAuthProvider) Login(w http.ResponseWriter, r *http.Request) *user.User {
	w.WriteHeader(http.StatusUnauthorized)
	return nil
}

func TestCustomAuthenticationProvidersAndApplicationsAreIndependent(t *testing.T) {
	var providers []*customAuthProvider
	options := application.Options{
		Modules: authenticationModules,
		AuthProviders: []auth.Descriptor{{Name: "custom", New: func() auth.AuthService {
			p := &customAuthProvider{}
			providers = append(providers, p)
			return p
		}}},
	}
	var apps []*application.Application
	for range 2 {
		cfg := authenticationConfig(t)
		cfg.Auth.Services = map[string]map[string]string{"custom": {"enabled": "true"}}
		app, err := application.New(t.Context(), cfg, options)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { app.Shutdown() })
		apps = append(apps, app)
	}
	if len(providers) != 2 || providers[0] == providers[1] {
		t.Fatal("shared auth provider instances")
	}
	for _, app := range apps {
		request := browserRequests(t, app, false)
		if w := request(http.MethodGet, "/auth/login/custom", nil); w.Code != http.StatusNoContent {
			t.Fatal("custom provider was not connected", w.Code, w.Body)
		}
		if w := request(http.MethodGet, "/auth/login/email", nil); w.Code != http.StatusNotFound {
			t.Fatal("custom catalog fell back to email", w.Code, w.Body)
		}
	}
	if err := apps[1].Shutdown(); err != nil {
		t.Fatal(err)
	}
	if w := browserRequests(t, apps[0], false)(http.MethodGet, "/auth/login/custom", nil); w.Code != http.StatusNoContent {
		t.Fatal("closing another app affected this app", w.Code, w.Body)
	}
}

func TestAuthenticationFailuresCloseOwnedServices(t *testing.T) {
	for _, mode := range []string{"no sessions", "nonrevocable sessions", "verification without mail", "empty providers", "unknown provider", "provider init", "duplicate auth", "login URL", "home URL"} {
		t.Run(mode, func(t *testing.T) {
			cfg := authenticationConfig(t)
			options := application.Options{}
			cause := errors.New("custom provider failed")
			switch mode {
			case "no sessions":
				cfg.HTTP.Session.Types, cfg.HTTP.Session.Stores = nil, nil
			case "nonrevocable sessions":
				cfg.HTTP.Session.Types = map[string]string{"default": "cookiestore"}
				cfg.HTTP.Session.Stores = map[string]map[string]string{"cookiestore": {"enabled": "true"}}
			case "verification without mail":
				cfg.Auth.RegisterRequiresVerification = true
			case "empty providers":
				options.AuthProviders = []auth.Descriptor{}
			case "unknown provider":
				cfg.Auth.Services = map[string]map[string]string{"unknown": {"enabled": "true"}}
			case "provider init":
				cfg.Auth.Services = map[string]map[string]string{"custom": {"enabled": "true"}}
				options.AuthProviders = []auth.Descriptor{{Name: "custom", New: func() auth.AuthService { return &customAuthProvider{initErr: cause} }}}
			}
			var captured *application.Application
			options.Modules = func(ctx context.Context, app *application.Application) ([]handlers.Descriptor, error) {
				captured = app
				catalog, err := authenticationModules(ctx, app)
				switch mode {
				case "duplicate auth":
					catalog = append(catalog, catalog[0])
				case "login URL":
					app.ContentManager.AuthURL = "https://other.example"
				case "home URL":
					app.ContentManager.HomeURL = "//other.example"
				}
				return catalog, err
			}
			app, err := application.New(t.Context(), cfg, options)
			if app != nil || err == nil || captured == nil {
				t.Fatalf("bad auth setup: app=%v err=%v captured=%v", app, err, captured)
			}
			if mode == "provider init" && !errors.Is(err, cause) {
				t.Fatal("lost provider error", err)
			}
			if _, err := captured.Accounts.GetByEmail(t.Context(), "reader@example.com"); err == nil || !strings.Contains(err.Error(), "database is closed") {
				t.Fatal("account storage remained open after failure", err)
			}
			if mode != "no sessions" && mode != "nonrevocable sessions" {
				if err := captured.Sessions.CheckHealth(t.Context()); err == nil {
					t.Fatal("sessions remained open after failure")
				}
			}
		})
	}
}

func TestDisabledAuthenticationIsRemovedBeforeModuleSetup(t *testing.T) {
	for _, requiresAuth := range []bool{false, true} {
		t.Run(fmt.Sprintf("requires auth=%t", requiresAuth), func(t *testing.T) {
			var featureCreated bool
			catalog := []handlers.Descriptor{
				{Name: auth.ModuleName, Provides: []string{"identity"}, New: func() handlers.Handler {
					t.Fatal("disabled auth factory ran")
					return nil
				}},
				{Name: "feature", New: func() handlers.Handler {
					featureCreated = true
					return &moduleStub{routes: func(routes *routing.Routes) error {
						routes.HandleFunc("GET /feature", func(w http.ResponseWriter, r *http.Request) {
							w.WriteHeader(http.StatusNoContent)
						})
						return routes.Err()
					}}
				}},
			}
			if requiresAuth {
				catalog[1].Requires = []handlers.Requirement{{Capability: "identity"}}
			}
			authFactory := reflect.ValueOf(catalog[0].New).Pointer()
			app, err := application.New(t.Context(), config.Config{}, application.Options{
				Modules: func(context.Context, *application.Application) ([]handlers.Descriptor, error) {
					return catalog, nil
				},
			})
			if app != nil {
				t.Cleanup(func() { app.Shutdown() })
			}
			if catalog[0].Name != auth.ModuleName || catalog[0].New == nil || reflect.ValueOf(catalog[0].New).Pointer() != authFactory || catalog[1].Name != "feature" {
				t.Fatal("modified the supplied catalog")
			}
			if requiresAuth {
				if app != nil || err == nil || !strings.Contains(err.Error(), `module "feature" requires missing capability "identity"`) {
					t.Fatalf("expected missing dependency: app=%v err=%v", app, err)
				}
				if featureCreated {
					t.Fatal("created a module before resolving dependencies")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if app.Accounts != nil || app.Sessions != nil || app.ContentManager.LoadNotifications != nil {
				t.Fatal("disabled auth activated services or notifications")
			}
			w := browserRequests(t, app, false)(http.MethodGet, "/feature", nil)
			if w.Code != http.StatusNoContent {
				t.Fatalf("feature status = %d, want %d", w.Code, http.StatusNoContent)
			}
		})
	}
}

func TestAuthenticationSelectionAndPresentationOverrides(t *testing.T) {
	for _, mode := range []string{"unselected", "disabled", "selected"} {
		t.Run(mode, func(t *testing.T) {
			cfg := authenticationConfig(t)
			if mode == "disabled" {
				cfg.Auth.Enabled = false
			}
			var catalog []handlers.Descriptor
			var factory uintptr
			app, err := application.New(t.Context(), cfg, application.Options{Modules: func(ctx context.Context, app *application.Application) ([]handlers.Descriptor, error) {
				catalog, _ = authenticationModules(ctx, app)
				if mode == "unselected" {
					catalog = nil
				} else {
					factory = reflect.ValueOf(catalog[0].New).Pointer()
				}
				app.ContentManager.AuthURL, app.ContentManager.HomeURL = "/sign-in", "/home"
				app.ContentManager.BuildLayoutData = func(*http.Request) any { return "custom" }
				return catalog, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { app.Shutdown() })
			if len(catalog) > 0 && reflect.ValueOf(catalog[0].New).Pointer() != factory {
				t.Fatal("mutated the application's catalog")
			}
			if app.ContentManager.AuthURL != "/sign-in" || app.ContentManager.HomeURL != "/home" || app.ContentManager.BuildLayoutData(nil) != "custom" {
				t.Fatal("replaced presentation overrides")
			}
			w := browserRequests(t, app, false)(http.MethodGet, "/auth/login/email", nil)
			want := http.StatusNotFound
			if mode == "selected" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("status = %d, want %d: %s", w.Code, want, w.Body)
			}
			app.Web.HandleFunc("GET /session", func(w http.ResponseWriter, r *http.Request) {
				if session.GetSessionManager(r) != app.Sessions {
					t.Error("session loading missing")
				}
			})
			browserRequests(t, app, false)(http.MethodGet, "/session", nil)
		})
	}
}
