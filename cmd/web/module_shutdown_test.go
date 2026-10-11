package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

type lifecycleModule struct {
	orderedModule
	bindRoutes func(*routing.Routes) error
	close      func(context.Context) error
}

func (m *lifecycleModule) Routes(routes *routing.Routes) error {
	if m.bindRoutes != nil {
		return m.bindRoutes(routes)
	}
	return nil
}

func (m *lifecycleModule) Shutdown(ctx context.Context) error { return m.close(ctx) }

func TestSetupHandlersOwnsModuleCleanup(t *testing.T) {
	for _, mode := range []string{"success", "route error", "route conflict"} {
		t.Run(mode, func(t *testing.T) {
			app := &server.ApplicationServer{Config: &config.Config{}, Web: http.NewServeMux(), Database: shutdownTestDB(t)}
			t.Cleanup(func() { _ = app.Shutdown() })
			var steps, closed []string
			routeErr, closeErr := errors.New("routes failed"), errors.New("cleanup failed")
			newModule := func(name string) *lifecycleModule {
				return &lifecycleModule{orderedModule: orderedModule{name: name, steps: &steps}, close: func(ctx context.Context) error {
					closed = append(closed, name)
					if ctx.Err() != nil {
						t.Error("module cleanup inherited canceled context:", ctx.Err())
					}
					if err := app.Database.Ping(); err != nil {
						t.Error("module lost shared pool:", err)
					}
					if name == "consumer" {
						return closeErr
					}
					return nil
				}}
			}
			provider, consumer := newModule("provider"), newModule("consumer")
			consumer.bindRoutes = func(routes *routing.Routes) error {
				if mode == "route error" {
					return routeErr
				}
				if mode == "route conflict" {
					routes.HandleFunc("GET /same", func(http.ResponseWriter, *http.Request) {})
					routes.HandleFunc("GET /same", func(http.ResponseWriter, *http.Request) {})
				}
				return nil // SetupHandlers must also inspect Routes.Err.
			}
			catalog := []handlers.Descriptor{
				{Name: "consumer", New: func() handlers.Handler { return consumer }, Requires: []handlers.Requirement{{Capability: "storage"}}},
				{Name: "provider", New: func() handlers.Handler { return provider }, Provides: []string{"storage"}},
			}
			ctx, cancel := context.WithCancel(context.Background())
			err := SetupHandlers(ctx, app, nil, catalog, nil)
			cancel()
			if mode == "success" {
				if err != nil || len(closed) != 0 {
					t.Fatalf("premature cleanup: %v %v", closed, err)
				}
				if err := app.Shutdown(); !errors.Is(err, closeErr) {
					t.Fatal("lost cleanup error:", err)
				}
			} else {
				if !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "register module") {
					t.Fatal("startup did not include route and cleanup errors:", err)
				}
				if mode == "route error" && !errors.Is(err, routeErr) {
					t.Fatal("lost route error:", err)
				}
			}
			if strings.Join(closed, ",") != "consumer,provider" {
				t.Fatal("cleanup order:", closed)
			}
			if err := app.Shutdown(); err != nil || len(closed) != 2 {
				t.Fatalf("repeated cleanup=%v error=%v", closed, err)
			}
		})
	}
}
