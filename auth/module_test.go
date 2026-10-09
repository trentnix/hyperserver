package auth_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

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
	cfg := config.Config{
		Auth: config.AuthConfig{Enabled: true, JwtKey: "test-key", VerificationTokenExpiration: time.Hour,
			ResetTokenExpiration: time.Hour, Services: map[string]map[string]string{"custom": {"enabled": "true"}}},
	}
	cfg.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
	return cfg
}

func TestModuleOwnsAuthenticationSetup(t *testing.T) {
	providers := []auth.Descriptor{{Name: "custom", New: func() auth.AuthService { return new(moduleProvider) }}}
	descriptor := auth.Module(providers)
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
		module := auth.Module(nil).New()
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

func TestModuleRejectsProviderRouteConflict(t *testing.T) {
	app, err := server.NewApplicationServer(moduleConfig())
	if err != nil {
		t.Fatal(err)
	}
	app.Web.HandleFunc("GET /provider", func(http.ResponseWriter, *http.Request) {})
	module := auth.Module([]auth.Descriptor{{Name: "custom", New: func() auth.AuthService { return new(moduleProvider) }}}).New()
	if err := module.Init(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	err = module.Routes(routing.NewRoutes(app.Web))
	if err == nil || !strings.Contains(err.Error(), `auth service "custom"`) || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("provider conflict = %v", err)
	}
}
