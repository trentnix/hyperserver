package auth_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAuthenticationProviderDiagnostics(t *testing.T) {
	for _, mode := range []string{"enabled", "disabled", "failure"} {
		t.Run(mode, func(t *testing.T) {
			cfg := moduleConfig()
			cfg.Auth.Enabled = mode != "disabled"
			cfg.Auth.Services["custom"]["password"] = "private-provider-secret"
			cfg.Auth.JwtKey = "private-signing-secret"
			app, err := server.NewApplicationServer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var accounts user.AccountRepository = &struct{ user.AccountRepository }{}
			if mode == "failure" {
				accounts = nil
			}
			sessions := moduleSessions(t, map[string]string{"default": "custom"})
			module := auth.Module(accounts, sessions, []auth.Descriptor{{Name: "custom", New: func() auth.AuthService { return new(moduleProvider) }}}).New()
			var output bytes.Buffer
			var log logger.Logger = logger.NewSlogLogger(slog.NewTextHandler(&output, nil))
			err = module.Init(logger.Set(t.Context(), &log), app)
			if mode == "failure" {
				if err == nil || output.Len() != 0 {
					t.Fatalf("failed setup emitted success: error=%v log=%s", err, output.String())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "providers=[custom]"
			if mode == "disabled" {
				want = "providers=[]"
			}
			if !strings.Contains(output.String(), want) || strings.Contains(output.String(), "private-") {
				t.Fatalf("incorrect provider diagnostics: %s", output.String())
			}
		})
	}
}

type moduleProvider struct {
	auth.AuthService
	name string
}

func (*moduleProvider) AuthType() string { return "custom" }
func (p *moduleProvider) Init(app *server.ApplicationServer) error {
	p.name = app.Config.App.Name
	return nil
}
func (p *moduleProvider) Routes(routes *routing.Routes) error {
	routes.HandleFunc("GET /provider", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, p.name)
	})
	return nil
}

func moduleConfig() config.Config {
	return config.Config{
		Auth: config.AuthConfig{Enabled: true, JwtKey: "test-key", VerificationTokenExpiration: time.Hour,
			ResetTokenExpiration: time.Hour, Services: map[string]map[string]string{"custom": {"enabled": "true"}}},
	}
}

type moduleSessionStore struct{ session.SessionStore }

func (moduleSessionStore) IsEnabled() bool { return true }

type moduleRotatingStore struct {
	moduleSessionStore
	session.RotatingStore
}

func moduleSessions(t *testing.T, types map[string]string) *session.SessionManager {
	t.Helper()
	manager, err := session.NewSessionManager(t.Context(), types, map[string]session.StoreFactory{
		"custom": func(context.Context) (session.SessionStore, error) { return moduleRotatingStore{}, nil },
		"cookie": func(context.Context) (session.SessionStore, error) { return moduleSessionStore{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	return manager
}

func TestModuleOwnsAuthenticationSetup(t *testing.T) {
	providers := []auth.Descriptor{{Name: "custom", New: func() auth.AuthService { return new(moduleProvider) }}}
	sessions := moduleSessions(t, map[string]string{"default": "custom"})
	descriptor := auth.Module(&struct{ user.AccountRepository }{}, sessions, providers)
	providers[0].New = nil // Module must retain a catalog snapshot.
	var apps []*server.ApplicationServer
	for _, name := range []string{"first", "second"} {
		cfg := moduleConfig()
		cfg.App.Name = name
		app, err := server.NewApplicationServer(cfg)
		if err != nil {
			t.Fatal(err)
		}
		module := descriptor.New()
		if err := module.Init(context.Background(), app); err != nil {
			t.Fatal(err)
		}
		if err := module.Routes(routing.NewRoutes(app.Web)); err != nil {
			t.Fatal(err)
		}
		apps = append(apps, app)
	}

	for _, app := range apps {
		w := httptest.NewRecorder()
		app.Web.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/provider", nil))
		if w.Code != http.StatusOK || w.Body.String() != app.Config.App.Name {
			t.Fatalf("provider response = %d %q", w.Code, w.Body.String())
		}
		_, pattern := app.Web.Handler(httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
		if pattern != "POST /auth/logout" {
			t.Fatalf("manager routes not bound: %q", pattern)
		}
	}
}

func TestModuleProviderSelection(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		cfg := moduleConfig()
		cfg.Auth.Enabled = enabled
		app, err := server.NewApplicationServer(cfg)
		if err != nil {
			t.Fatal(err)
		}
		module := auth.Module(nil, nil, nil).New()
		err = module.Init(context.Background(), app)
		if enabled {
			if err == nil || !strings.Contains(err.Error(), "unknown enabled") {
				t.Fatalf("empty local catalog: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := module.Routes(routing.NewRoutes(app.Web)); err != nil {
			t.Fatal(err)
		}
		_, pattern := app.Web.Handler(httptest.NewRequest(http.MethodGet, "/auth/login", nil))
		if pattern != "" {
			t.Fatalf("disabled authentication registered %q", pattern)
		}
	}
}

func TestModuleRequiresAccountRepository(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			cfg := moduleConfig()
			cfg.Auth.Enabled = enabled
			cfg.App.Name = "repository requirement test"
			app, err := server.NewApplicationServer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			sessions := moduleSessions(t, map[string]string{"default": "custom"})
			provider := new(moduleProvider)
			descriptor := auth.Module(nil, sessions, []auth.Descriptor{{Name: "custom", New: func() auth.AuthService { return provider }}})
			err = handlers.Setup(t.Context(), app, nil, []handlers.Descriptor{descriptor}, nil)
			if enabled {
				if err == nil || !strings.Contains(err.Error(), "requires an account repository") {
					t.Fatalf("missing repository: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if provider.name != "" {
				t.Fatal("provider initialized without an account repository")
			}
			if _, pattern := app.Web.Handler(httptest.NewRequest(http.MethodPost, "/auth/logout", nil)); pattern != "" {
				t.Fatalf("registered auth route without an account repository: %q", pattern)
			}
			t.Cleanup(func() {
				if err := app.Shutdown(); err != nil {
					t.Error(err)
				}
			})
		})
	}
}

func TestModuleRejectsProviderRouteConflict(t *testing.T) {
	app, err := server.NewApplicationServer(moduleConfig())
	if err != nil {
		t.Fatal(err)
	}
	app.Web.HandleFunc("GET /provider", func(http.ResponseWriter, *http.Request) {})
	sessions := moduleSessions(t, map[string]string{"default": "custom"})
	module := auth.Module(&struct{ user.AccountRepository }{}, sessions, []auth.Descriptor{{Name: "custom", New: func() auth.AuthService { return new(moduleProvider) }}}).New()
	if err := module.Init(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	err = module.Routes(routing.NewRoutes(app.Web))
	if err == nil || !strings.Contains(err.Error(), `auth service "custom"`) || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("provider conflict = %v", err)
	}
}

func TestModuleRequiresSelectedStoreRotation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types map[string]string
		valid bool
	}{
		{"custom fallback", map[string]string{"default": "custom"}, true},
		{"custom override", map[string]string{"default": "cookie", "auth-user-session": "custom"}, true},
		{"cookie fallback", map[string]string{"default": "cookie"}, false},
		{"cookie override", map[string]string{"default": "custom", "auth-user-session": "cookie"}, false},
		{"missing selection", map[string]string{}, false},
		{"missing manager", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, err := server.NewApplicationServer(moduleConfig())
			if err != nil {
				t.Fatal(err)
			}
			var sessions *session.SessionManager
			if tc.types != nil {
				sessions = moduleSessions(t, tc.types)
			}
			module := auth.Module(&struct{ user.AccountRepository }{}, sessions, []auth.Descriptor{{Name: "custom", New: func() auth.AuthService { return new(moduleProvider) }}}).New()
			err = module.Init(t.Context(), app)
			if tc.valid && err != nil {
				t.Fatal(err)
			}
			if !tc.valid && (err == nil || !strings.Contains(err.Error(), "session revocation")) {
				t.Fatalf("initialization error = %v, want session revocation requirement", err)
			}
		})
	}
}

func TestModuleValidatesInjectedSessions(t *testing.T) {
	for _, configuredDefault := range []string{"", "sqliteStore"} {
		for _, tc := range []struct {
			name  string
			types map[string]string
			want  string
		}{
			{"default", map[string]string{"default": "custom"}, ""},
			{"named sessions", map[string]string{"auth-user-session": "custom", session.AuthSession: "cookie"}, ""},
			{"missing redirect session", map[string]string{"auth-user-session": "custom"}, "authentication requires a redirect session"},
			{"missing identity session", map[string]string{session.AuthSession: "cookie"}, "authentication requires session revocation"},
		} {
			t.Run(fmt.Sprintf("config default=%q/%s", configuredDefault, tc.name), func(t *testing.T) {
				cfg := moduleConfig()
				cfg.HTTP.Session.Types = nil
				if configuredDefault != "" {
					cfg.HTTP.Session.Types = map[string]string{"default": configuredDefault}
				}
				app, err := server.NewApplicationServer(cfg)
				if err != nil {
					t.Fatal(err)
				}
				sessions := moduleSessions(t, tc.types)
				module := auth.Module(&struct{ user.AccountRepository }{}, sessions, []auth.Descriptor{{Name: "custom", New: func() auth.AuthService { return new(moduleProvider) }}}).New()

				err = module.Init(t.Context(), app)
				if tc.want == "" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("initialization error = %v, want %q", err, tc.want)
				}
			})
		}
	}
}
