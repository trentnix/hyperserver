//go:build cgo

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/examples/stateless/pages"
	site "github.com/trentnix/hyperserver/modules/site"
	"github.com/trentnix/hyperserver/modules/site/storage"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestCompetingRenderingPoliciesFailInEitherOrder(t *testing.T) {
	siteDefaults := func(cm *contentservice.ContentManagerService) error { return site.ConfigureRendering(cm, false) }
	for _, policies := range [][]func(*contentservice.ContentManagerService) error{
		{siteDefaults, pages.ConfigureRendering},
		{pages.ConfigureRendering, siteDefaults},
	} {
		cm := contentservice.NewContentManager()
		if err := policies[0](cm); err != nil {
			t.Fatal(err)
		}
		layout := cm.Layouts[contentservice.PageType][0].Path
		if err := policies[1](cm); err == nil {
			t.Fatal("competing rendering policies accepted")
		}
		if got := cm.Layouts[contentservice.PageType]; len(got) != 1 || got[0].Path != layout {
			t.Fatal("rejected policy changed layouts")
		}
	}
}

// These integration tests include the reference site's SQLite storage.
func TestApplicationSelectsOnlyItsModule(t *testing.T) {
	// The test imports the actual site module. Both modules are discoverable,
	// but the stateless application must never activate the site's routes.
	names := make(map[string]bool)
	for _, descriptor := range handlers.Registered() {
		names[descriptor.Name] = true
	}
	if !names["site"] || !names["hello"] {
		t.Fatal("expected imported modules to self-register")
	}
	app, err := newApplication(context.Background(), "Hello")
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/contact", http.StatusNotFound},
		{http.MethodGet, "/auth/login", http.StatusNotFound},
		{http.MethodPost, "/test-email", http.StatusNotFound},
		{http.MethodPost, "/", http.StatusMethodNotAllowed},
	} {
		w := httptest.NewRecorder()
		app.Web.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: status=%d, want %d", tc.method, tc.path, w.Code, tc.status)
		}
	}
}

func TestApplicationRunsBesideReferenceSite(t *testing.T) {
	for _, stopSite := range []bool{false, true} {
		t.Run(map[bool]string{true: "stop site", false: "stop hello"}[stopSite], func(t *testing.T) {
			t.Chdir(t.TempDir())
			reference, err := server.NewApplicationServer(config.Config{
				App: config.AppConfig{Name: "Reference site"},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reference.Shutdown() })
			db, err := database.Setup("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			reference.OnShutdown("database", func(context.Context) error { return db.Close() })
			newContacts := func(ctx context.Context) (site.ContactRepository, error) {
				return storage.NewSQLiteContactRepository(ctx, db.DB)
			}
			if err := site.ConfigureRendering(reference.ContentManager, false); err != nil {
				t.Fatal(err)
			}
			modules, err := handlers.Initialize(context.Background(), reference, []handlers.Descriptor{site.Module(newContacts, nil)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			reference.OnShutdown("modules", func(ctx context.Context) error { return handlers.Close(ctx, modules) })
			routes := routing.NewRoutes(reference.Web)
			for _, module := range modules {
				if err := module.Routes(routes); err != nil {
					t.Fatal(err)
				}
			}
			if err := routes.Err(); err != nil {
				t.Fatal(err)
			}
			hello, err := newApplication(context.Background(), "Independent greeting")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = hello.Shutdown() })
			check := func(app *server.ApplicationServer) {
				for range 10 {
					w := httptest.NewRecorder()
					app.Web.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
					if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), app.Config.App.Name) {
						t.Errorf("application templates crossed: status=%d body=%s", w.Code, w.Body.String())
					}
				}
			}
			var requests sync.WaitGroup
			requests.Go(func() { check(reference) })
			requests.Go(func() { check(hello) })
			requests.Wait()

			stopped, survivor := hello, reference
			if stopSite {
				stopped, survivor = reference, hello
			}
			if err := stopped.Shutdown(); err != nil {
				t.Fatal(err)
			}
			check(survivor)
			if !stopSite {
				if err := db.Ping(); err != nil {
					t.Fatal("stateless shutdown closed the site's database:", err)
				}
			}
		})
	}
}
