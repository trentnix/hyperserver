package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

type setupModule struct {
	closingModule
	routes func(*routing.Routes) error
}

func (m *setupModule) Routes(routes *routing.Routes) error { return m.routes(routes) }

func TestSetupPreparesSharedTemplatesBeforeModules(t *testing.T) {
	for _, mode := range []string{"success", "failure", "already canceled", "canceled during preparation"} {
		t.Run(mode, func(t *testing.T) {
			app, err := server.NewApplicationServer(config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause := errors.New("template preparation failed")
			var calls []string
			app.ContentManager.PrepareTemplates = func() error {
				calls = append(calls, "prepare")
				if app.ContentManager.HomeURL != "/override" {
					t.Fatal("preparation ran before overrides")
				}
				if mode == "failure" {
					return cause
				}
				if mode == "canceled during preparation" {
					cancel()
				}
				return nil
			}
			app.ContentManager.HomeURL = "/override"
			module := &setupModule{routes: func(*routing.Routes) error { calls = append(calls, "routes"); return nil }}
			module.init = func(context.Context) error { calls = append(calls, "init"); return nil }
			module.close = func(context.Context) error { return nil }
			catalog := []handlers.Descriptor{{Name: "test", New: func() handlers.Handler {
				calls = append(calls, "factory")
				return module
			}}}
			if mode == "already canceled" {
				cancel()
			}
			err = handlers.Setup(ctx, app, nil, catalog, nil)
			want := "prepare"
			switch mode {
			case "success":
				want = "prepare,factory,init,routes"
				if err != nil {
					t.Fatal(err)
				}
			case "failure":
				if !errors.Is(err, cause) {
					t.Fatalf("lost preparation error: %v", err)
				}
			default:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
				if mode == "already canceled" {
					want = ""
				}
			}
			if got := strings.Join(calls, ","); got != want {
				t.Fatalf("calls=%s, want %s", got, want)
			}
			if err := app.Shutdown(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModuleTemplateFailureRollsBackStartup(t *testing.T) {
	app, err := server.NewApplicationServer(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	closed := 0
	provider := &setupModule{routes: func(*routing.Routes) error { t.Fatal("routes bound after template failure"); return nil }}
	provider.provide = func() map[string]any { return map[string]any{"ready": provider} }
	provider.close = func(context.Context) error { closed++; return nil }
	consumer := &setupModule{}
	consumer.init = func(context.Context) error {
		_, err := content.PrepareTemplates(app.ContentManager, content.TemplatePath{FS: fstest.MapFS{"broken.html": {Data: []byte("{{if}}")}}, Path: "broken.html"})
		return err
	}
	catalog := []handlers.Descriptor{
		{Name: "provider", Provides: []string{"ready"}, New: func() handlers.Handler { return provider }},
		{Name: "consumer", Requires: []handlers.Requirement{{Capability: "ready"}}, New: func() handlers.Handler { return consumer }},
	}
	err = handlers.Setup(t.Context(), app, nil, catalog, nil)
	if !errors.Is(err, content.ErrParsingTemplates) || closed != 1 {
		t.Fatalf("error=%v closed=%d", err, closed)
	}
	if err := app.Shutdown(); err != nil || closed != 1 {
		t.Fatalf("repeated cleanup: error=%v closed=%d", err, closed)
	}
}

func TestSetupOwnsRoutesAndCleanup(t *testing.T) {
	for _, mode := range []string{"defaults", "custom routes", "route error", "route conflict", "canceled route"} {
		t.Run(mode, func(t *testing.T) {
			app, err := server.NewApplicationServer(config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause, closeErr := errors.New("route failure"), errors.New("close failure")
			var closed []string
			module := func(name string) *setupModule {
				m := &setupModule{routes: func(*routing.Routes) error { return nil }}
				m.close = func(ctx context.Context) error {
					if ctx.Err() != nil {
						t.Error("cleanup inherited cancellation")
					}
					closed = append(closed, name)
					if name == "consumer" {
						return closeErr
					}
					return nil
				}
				return m
			}
			provider, consumer := module("provider"), module("consumer")
			provider.provide = func() map[string]any { return map[string]any{"storage": provider} }
			consumer.routes = func(routes *routing.Routes) error {
				routes.HandleFunc("POST /form", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
				switch mode {
				case "route error":
					return cause
				case "route conflict":
					routes.HandleFunc("POST /form", func(http.ResponseWriter, *http.Request) {})
				case "canceled route":
					cancel()
				}
				return nil
			}
			catalog := []handlers.Descriptor{
				{Name: "consumer", New: func() handlers.Handler { return consumer }, Requires: []handlers.Requirement{{Capability: "storage"}}},
				{Name: "provider", New: func() handlers.Handler { return provider }, Provides: []string{"storage"}},
			}
			var routes *routing.Routes
			if mode == "custom routes" {
				routes, err = routing.NewRoutes(app.Web).WithRateLimit(ratelimit.Policy{Requests: 1, Window: time.Second, MaxClients: 1})
				if err != nil {
					t.Fatal(err)
				}
			}
			err = handlers.Setup(ctx, app, routes, catalog, nil)
			if mode == "defaults" || mode == "custom routes" {
				if err != nil || len(closed) != 0 {
					t.Fatalf("setup: error=%v closed=%v", err, closed)
				}
				for i := range 2 {
					response := httptest.NewRecorder()
					app.Web.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/form", nil))
					want := http.StatusNoContent
					if mode == "custom routes" && i == 1 {
						want = http.StatusTooManyRequests
					}
					if response.Code != want {
						t.Fatalf("status=%d want=%d", response.Code, want)
					}
				}
				if err := app.Shutdown(); !errors.Is(err, closeErr) {
					t.Fatal("cleanup error lost:", err)
				}
			} else {
				if !errors.Is(err, closeErr) {
					t.Fatal("rollback error lost:", err)
				}
				if mode == "route error" && !errors.Is(err, cause) || mode == "canceled route" && !errors.Is(err, context.Canceled) || mode == "route conflict" && !strings.Contains(err.Error(), "conflicts") {
					t.Fatal("setup error lost:", err)
				}
			}
			if err := app.Shutdown(); err != nil || strings.Join(closed, ",") != "consumer,provider" {
				t.Fatalf("cleanup order or repeated cleanup: error=%v closed=%v", err, closed)
			}
		})
	}
}

func TestSetupRejectsInvalidApplicationAndRoutesBeforeFactories(t *testing.T) {
	for _, app := range []*server.ApplicationServer{nil, {}, {Config: &config.Config{}}} {
		if err := handlers.Setup(context.Background(), app, nil, nil, nil); err == nil {
			t.Fatal("invalid application accepted")
		}
	}
	app, err := server.NewApplicationServer(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	routes := routing.NewRoutes(app.Web)
	routes.Handle("GET /bad", nil)
	catalog := []handlers.Descriptor{{Name: "test", New: func() handlers.Handler {
		t.Fatal("factory called with invalid routes")
		return nil
	}}}
	if err := handlers.Setup(context.Background(), app, routes, catalog, nil); err == nil {
		t.Fatal("invalid routes accepted")
	}
}
