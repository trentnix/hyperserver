package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/config"
	site "github.com/trentnix/hyperserver/modules/site"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestStatelessApplication(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HYPERSERVER_HTTP_PORT", "invalid")
	app, err := newApplication(context.Background(), "Independent greeting")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	for _, htmx := range []bool{false, true} {
		for _, name := range []string{"", "Ada", "<script>alert(1)</script>"} {
			r := httptest.NewRequest(http.MethodGet, "/?name="+url.QueryEscape(name), nil)
			if htmx {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			app.Web.ServeHTTP(w, r)
			body := w.Body.String()
			if w.Code != http.StatusOK || strings.Contains(body, "<!doctype html>") == htmx {
				t.Fatalf("htmx=%t: status=%d body=%s", htmx, w.Code, body)
			}
			want := name
			if name == "" {
				want = "world"
			} else if strings.HasPrefix(name, "<script>") {
				want = "&lt;script&gt;alert(1)&lt;/script&gt;"
			}
			if !strings.Contains(body, "Hello, "+want+"!") || strings.Contains(body, "<script>") {
				t.Fatalf("incorrect or unescaped greeting: %s", body)
			}
			if !htmx && !strings.Contains(body, "Independent greeting") {
				t.Fatal("application name missing")
			}
			if len(w.Result().Cookies()) != 0 || w.Header().Get("Vary") != "HX-Request" {
				t.Fatal("response set a cookie or lost representation variation")
			}
		}
	}
	if app.Database != nil || app.SessionManager != nil || app.AccountRepository != nil || app.Mail != nil {
		t.Fatal("stateless application activated an optional service")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("application created files: entries=%v error=%v", entries, err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil || workingDirectory != directory {
		t.Fatalf("application changed working directory: %q, %v", workingDirectory, err)
	}
}

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

func TestApplicationStartupCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app, err := newApplication(ctx, "Hello")
	if app != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled startup: app=%v error=%v", app, err)
	}
}

func TestApplicationRenderFailure(t *testing.T) {
	app, err := newApplication(context.Background(), "Hello")
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	app.ContentManager.RegisterLayouts(contentservice.PageType, fstest.MapFS{
		"broken.html": {Data: []byte("private-template-marker {{if}}")},
	}, "broken.html")
	w := httptest.NewRecorder()
	app.Web.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusInternalServerError || w.Body.String() != "Unable to display greeting\n" {
		t.Fatalf("render failure leaked output: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestApplicationRunsBesideReferenceSite(t *testing.T) {
	for _, stopSite := range []bool{false, true} {
		t.Run(map[bool]string{true: "stop site", false: "stop hello"}[stopSite], func(t *testing.T) {
			t.Chdir(t.TempDir())
			reference, err := server.NewApplicationServer(config.Config{
				App:      config.AppConfig{Name: "Reference site"},
				Database: config.DatabaseConfig{Driver: "sqlite3", Connection: ":memory:"},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reference.Shutdown() })
			if err := reference.InitializeDatabase(context.Background()); err != nil {
				t.Fatal(err)
			}
			modules, err := handlers.Initialize(context.Background(), reference, []handlers.Descriptor{site.Module(nil)}, nil)
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
				if err := reference.Database.Ping(); err != nil {
					t.Fatal("stateless shutdown closed the site's database:", err)
				}
			}
		})
	}
}
