package web_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/auth/web"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type accountReader struct {
	user.AccountRepository
	name  string
	reads int
}

func (a *accountReader) GetByID(context.Context, string) (*user.User, error) {
	a.reads++
	return &user.User{ID: a.name, Email: a.name, SessionVersion: 1}, nil
}

type sessionStore struct {
	session.RotatingStore
	accountID string
	closed    int
}

func (s *sessionStore) Close() error { s.closed++; return nil }
func (s *sessionStore) New(_ *http.Request, name string) (*session.Session, error) {
	return session.NewSession(s, name), nil
}
func (s *sessionStore) Get(r *http.Request, name string) (*session.Session, error) {
	value, _ := s.New(r, name)
	if name == "auth-user-session" && s.accountID != "" {
		value.Data["auth-user-session"] = s.accountID
		value.Data["auth-session-version"] = int64(1)
	}
	return value, nil
}
func (*sessionStore) Save(http.ResponseWriter, *http.Request, *session.Session) error { return nil }
func (*sessionStore) End(http.ResponseWriter, *http.Request, *session.Session) error  { return nil }

type module struct {
	initErr error
	routes  func(*routing.Routes) error
	closed  int
}

func (m *module) Init(context.Context, *server.ApplicationServer) error { return m.initErr }
func (m *module) Routes(r *routing.Routes) error {
	if m.routes != nil {
		return m.routes(r)
	}
	return nil
}
func (m *module) Shutdown(context.Context) error { m.closed++; return nil }

func fixture(t *testing.T) (*server.ApplicationServer, web.Email, *sessionStore) {
	t.Helper()
	app, err := server.NewApplicationServer(config.Config{
		HTTP: config.HTTPConfig{PublicOrigin: "https://example.test"},
		Auth: config.AuthConfig{
			Enabled: true, JwtKey: strings.Repeat("k", 32),
			ResetTokenExpiration: time.Hour, VerificationTokenExpiration: time.Hour,
			Services: map[string]map[string]string{"email": {"enabled": "true"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.ContentManager.RegisterLayouts(content.PageType, fstest.MapFS{
		"page.html": {Data: []byte(`{{renderPartial .PartialName .}}`)},
	}, "page.html")
	store := new(sessionStore)
	sessions, err := session.NewSessionManager(t.Context(), map[string]string{"default": "test"}, map[string]session.StoreFactory{
		"test": func(context.Context) (session.SessionStore, error) { return store, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(app.Shutdown(), sessions.Close()); err != nil {
			t.Error(err)
		}
	})
	return app, web.Email{
		Accounts: &accountReader{name: t.Name()}, Sessions: sessions,
		LoginURL: "/auth/login/email", HomeURL: "/notes",
	}, store
}

func selectedAuth() handlers.Descriptor {
	return handlers.Descriptor{Name: auth.ModuleName, New: func() handlers.Handler { panic("unconfigured auth factory called") }}
}

func TestEmailSetupConnectsModulesAndIdentity(t *testing.T) {
	var checks []func()
	catalogSize := len(handlers.Registered())
	for _, name := range []string{"first", "second"} {
		app, setup, store := fixture(t)
		accounts := setup.Accounts.(*accountReader)
		accounts.name = name
		app.ContentManager.BuildLayoutData = func(*http.Request) any { return name }
		feature := &module{routes: func(routes *routing.Routes) error {
			routes.Handle("GET /private", setup.RequireAuthentication(app.ContentManager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if session.GetSessionManager(r) != setup.Sessions {
					t.Error("wrong application's session manager")
				}
				fmt.Fprint(w, user.GetUserFromContext(r.Context()).Email)
			})))
			return routes.Err()
		}}
		catalog := []handlers.Descriptor{selectedAuth(), {Name: "feature", New: func() handlers.Handler { return feature }}}
		originalFactory := reflect.ValueOf(catalog[0].New).Pointer()
		handler, err := setup.Setup(t.Context(), app, catalog)
		if err != nil {
			t.Fatal(err)
		}
		if accounts.reads != 0 || store.closed != 0 || app.ContentManager.BuildLayoutData(nil) != name {
			t.Fatal("setup accessed storage, closed services, or changed layout data")
		}
		if app.ContentManager.AuthURL != setup.LoginURL || app.ContentManager.HomeURL != setup.HomeURL ||
			reflect.ValueOf(app.ContentManager.LoadNotifications).Pointer() != reflect.ValueOf(flash.LoadNotifications).Pointer() {
			t.Fatal("missing redirect or notification setup")
		}
		if reflect.ValueOf(catalog[0].New).Pointer() != originalFactory {
			t.Fatal("setup changed the supplied catalog")
		}
		checks = append(checks, func() {
			for _, hx := range []bool{false, true} {
				r := httptest.NewRequest("GET", "/private", nil)
				if hx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				status, header := 303, "Location"
				if hx {
					status, header = 200, "HX-Redirect"
				}
				if w.Code != status || w.Header().Get(header) != setup.LoginURL {
					t.Fatalf("anonymous request: %d %v", w.Code, w.Header())
				}
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", setup.LoginURL, nil))
			if w.Code != 200 || !strings.Contains(w.Body.String(), `action="/auth/login/email"`) {
				t.Fatalf("email provider not connected: %d %s", w.Code, w.Body)
			}
			store.accountID = name
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/private", nil))
			if w.Code != 200 || w.Body.String() != name || accounts.reads != 1 {
				t.Fatalf("identity loading: %d %q reads=%d", w.Code, w.Body, accounts.reads)
			}
			if err := app.Shutdown(); err != nil || feature.closed != 1 || store.closed != 0 {
				t.Fatalf("cleanup ownership: error=%v module=%d store=%d", err, feature.closed, store.closed)
			}
		})
	}
	// Exercise both handlers after both apps exist to detect shared runtime state.
	for _, check := range checks {
		check()
	}
	if len(handlers.Registered()) != catalogSize {
		t.Fatal("setup changed the import catalog")
	}
}

func TestEmailSetupRejectsInvalidConfiguration(t *testing.T) {
	for _, name := range []string{
		"nil app", "nil config", "nil mux", "nil renderer", "disabled auth", "nil accounts", "typed nil accounts",
		"nil sessions", "missing flash store", "login URL", "home URL", "empty URL", "missing auth", "duplicate auth", "canceled",
		"rate limit requests", "rate limit window", "rate limit capacity",
	} {
		t.Run(name, func(t *testing.T) {
			app, setup, store := fixture(t)
			cm := app.ContentManager
			cm.AuthURL, cm.HomeURL = "/old-login", "/old-home"
			input := app
			catalog := []handlers.Descriptor{selectedAuth()}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch name {
			case "nil app":
				input = nil
			case "nil config":
				app.Config = nil
			case "nil mux":
				app.Web = nil
			case "nil renderer":
				app.ContentManager = nil
			case "disabled auth":
				app.Config.Auth.Enabled = false
			case "nil accounts":
				setup.Accounts = nil
			case "typed nil accounts":
				setup.Accounts = (*accountReader)(nil)
			case "nil sessions":
				setup.Sessions = nil
			case "missing flash store":
				setup.Sessions = new(session.SessionManager)
			case "login URL":
				setup.LoginURL = "https://example.test/login"
			case "home URL":
				setup.HomeURL = "/%2fexample.test"
			case "empty URL":
				setup.HomeURL = ""
			case "missing auth":
				catalog = nil
			case "duplicate auth":
				catalog = append(catalog, selectedAuth())
			case "canceled":
				cancel()
			case "rate limit requests", "rate limit window", "rate limit capacity":
				limit := config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 10}
				switch name {
				case "rate limit requests":
					limit.Requests = 0
				case "rate limit window":
					limit.Window = 0
				case "rate limit capacity":
					limit.MaxClients = -1
				}
				app.Config.HTTP.DefaultRateLimit = limit
			}
			handler, err := setup.Setup(ctx, input, catalog)
			if err == nil || handler != nil {
				t.Fatal("invalid setup returned a handler")
			}
			if cm.AuthURL != "/old-login" || cm.HomeURL != "/old-home" || cm.LoadNotifications != nil || store.closed != 0 {
				t.Fatal("invalid setup changed rendering or closed services")
			}
		})
	}
}

func TestEmailSetupConfiguredRouteLimits(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			var apps []http.Handler
			for range 2 {
				app, setup, _ := fixture(t)
				app.Config.HTTP.DefaultRateLimit = config.RateLimitConfig{
					Enabled: enabled, Requests: 1, Window: time.Minute, MaxClients: 10,
				}
				feature := &module{routes: func(routes *routing.Routes) error {
					serve := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
					routes.HandleFunc("GET /default", serve)
					routes.HandleFunc("GET /other", serve)
					scope, err := routes.WithRateLimit(ratelimit.Policy{Requests: 2, Window: time.Minute, MaxClients: 10})
					if err != nil {
						return err
					}
					scope.HandleFunc("GET /override", serve)
					scope.WithoutRateLimit().HandleFunc("GET /unlimited", serve)
					return routes.Err()
				}}
				handler, err := setup.Setup(t.Context(), app, []handlers.Descriptor{
					selectedAuth(), {Name: "feature", New: func() handlers.Handler { return feature }},
				})
				if err != nil {
					t.Fatal(err)
				}
				apps = append(apps, handler)
			}

			// Exercise both apps after setup to detect shared counters.
			for _, handler := range apps {
				for _, path := range []string{"/default", "/other", "/override", "/unlimited"} {
					for attempt := 1; attempt <= 3; attempt++ {
						w := httptest.NewRecorder()
						handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
						want := http.StatusNoContent
						if (enabled && (path == "/default" || path == "/other") && attempt > 1) ||
							(path == "/override" && attempt > 2) {
							want = http.StatusTooManyRequests
						}
						if w.Code != want {
							t.Fatalf("%s attempt %d: status=%d, want %d", path, attempt, w.Code, want)
						}
					}
				}
			}
		})
	}
}

func TestEmailSetupLogsSharedBudgetWarning(t *testing.T) {
	app, setup, _ := fixture(t)
	app.Config.HTTP.DefaultRateLimit = config.RateLimitConfig{Enabled: true, Requests: 2, Window: time.Minute, MaxClients: 10}
	app.Config.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 10}
	feature := &module{routes: func(routes *routing.Routes) error {
		routes.HandleFunc("GET /feature", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		return routes.Err()
	}}
	var logs bytes.Buffer
	ctx := logger.Set(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))
	handler, err := setup.Setup(ctx, app, []handlers.Descriptor{
		selectedAuth(), {Name: "feature", New: func() handlers.Handler { return feature }},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `route="GET /feature"`) || !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("missing shared-budget warning: %s", &logs)
	}
	// Setup must not install the separate shared-budget middleware.
	for range 2 {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/feature", nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("unexpected shared limit: %d", w.Code)
		}
	}
}

func TestEmailSetupRollsBackModulesNotServices(t *testing.T) {
	for _, failure := range []string{"init", "routes", "templates", "mail", "provider", "rotation"} {
		t.Run(failure, func(t *testing.T) {
			app, setup, store := fixture(t)
			resource := new(module)
			broken := new(module)
			cause := errors.New("fixture failure")
			catalog := []handlers.Descriptor{
				{Name: "a-resource", New: func() handlers.Handler { return resource }},
				selectedAuth(),
				{Name: "z-broken", New: func() handlers.Handler { return broken }},
			}
			switch failure {
			case "init":
				broken.initErr = cause
			case "routes":
				broken.routes = func(r *routing.Routes) error {
					r.HandleFunc("GET /auth/login", func(http.ResponseWriter, *http.Request) {})
					return r.Err()
				}
			case "templates":
				app.ContentManager.PrepareTemplates = func() error { return cause }
			case "mail":
				app.Config.Auth.RegistrationEnabled, app.Config.Auth.RegisterRequiresVerification = true, true
			case "provider":
				app.Config.Auth.Services["email"]["enabled"] = "false"
			case "rotation":
				sessions, err := session.NewSessionManager(t.Context(), map[string]string{"default": "plain"}, map[string]session.StoreFactory{
					"plain": func(context.Context) (session.SessionStore, error) {
						return &struct{ session.SessionStore }{store}, nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { sessions.Close() })
				setup.Sessions = sessions
			}
			handler, err := setup.Setup(t.Context(), app, catalog)
			if err == nil || handler != nil {
				t.Fatal("failed setup returned a handler")
			}
			if (failure == "init" || failure == "templates") && !errors.Is(err, cause) {
				t.Fatalf("lost cause: %v", err)
			}
			wantClosed := 1
			if failure == "templates" {
				wantClosed = 0
			}
			if resource.closed != wantClosed || store.closed != 0 {
				t.Fatalf("rollback: module=%d store=%d error=%v", resource.closed, store.closed, err)
			}
			if err := app.Shutdown(); err != nil || resource.closed != wantClosed || store.closed != 0 {
				t.Fatal("shutdown repeated cleanup or closed supplied services", err)
			}
		})
	}
}
