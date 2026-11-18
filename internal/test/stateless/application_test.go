package stateless_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

type greeting struct{ view *content.View }

func (m *greeting) Init(_ context.Context, app *server.ApplicationServer) error {
	files := fstest.MapFS{"greeting.html": {Data: []byte(`{{define "greeting"}}<p>{{.Data}}</p>{{end}}`)}}
	var err error
	m.view, err = content.PrepareView(app.ContentManager, "greeting", content.TemplatePath{FS: files, Path: "greeting.html"})
	return err
}

func (m *greeting) Routes(routes *routing.Routes) error {
	routes.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		page := m.view.Content(r, r.URL.Query().Get("name"))
		if err := page.Render(w, r); err != nil {
			page.HandleRenderError(w, r, "Unable to display greeting", err)
		}
	})
	return routes.Err()
}

// TestApplicationWithoutStorage runs with CGO disabled in CI.
func TestApplicationWithoutStorage(t *testing.T) {
	app, err := application.New(t.Context(), config.Config{}, application.Options{
		Modules: func(_ context.Context, app *application.Application) ([]handlers.Descriptor, error) {
			layouts := fstest.MapFS{
				"page.html":     {Data: []byte(`<!doctype html><html><body>{{renderPartial .PartialName .}}</body></html>`)},
				"fragment.html": {Data: []byte(`{{renderPartial .PartialName .}}`)},
			}
			app.ContentManager.AddPageLayout(layouts, "page.html")
			app.ContentManager.AddHtmxLayout(layouts, "fragment.html")
			return []handlers.Descriptor{{Name: "greeting", New: func() handlers.Handler { return &greeting{} }}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	if app.Accounts != nil || app.Sessions != nil || app.Mail != nil {
		t.Fatal("stateless application initialized optional services")
	}
	for _, tc := range []struct {
		name                string
		htmx, history, full bool
	}{
		{name: "ordinary", full: true},
		{name: "HTMX", htmx: true},
		{name: "history", htmx: true, history: true, full: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/?name=%3Cunsafe%3E", nil)
			if tc.htmx {
				request.Header.Set("HX-Request", "true")
			}
			if tc.history {
				request.Header.Set("HX-History-Restore-Request", "true")
			}
			response := httptest.NewRecorder()
			app.Handler.ServeHTTP(response, request)
			body := response.Body.String()
			if response.Code != http.StatusOK || !strings.Contains(body, "&lt;unsafe&gt;") || strings.Contains(body, "<html>") != tc.full {
				t.Fatalf("status=%d, body=%s", response.Code, body)
			}
		})
	}
}

func TestApplicationDoesNotImportStorageImplementations(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps", "github.com/trentnix/hyperserver/pkg/application").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect application dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		for _, forbidden := range []string{
			"github.com/mattn/go-sqlite3",
			"github.com/trentnix/hyperserver/pkg/database/sqlite",
			"github.com/trentnix/hyperserver/pkg/services/configured",
			"github.com/trentnix/hyperserver/pkg/services/user/sqlite",
			"github.com/trentnix/hyperserver/pkg/services/session/sqlite",
			"github.com/trentnix/hyperserver/pkg/services/session/cookie",
		} {
			if dependency == forbidden || strings.HasPrefix(dependency, forbidden+"/") {
				t.Errorf("application imports storage implementation %s", dependency)
			}
		}
	}
}

func TestUnselectedStorageProvidersFail(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*config.Config)
		want      string
	}{
		{"accounts", func(cfg *config.Config) {
			cfg.Auth.Enabled = true
			cfg.Auth.AccountStorage.Provider = "sqlite"
		}, `unknown provider "sqlite"`},
		{"missing account selection", func(cfg *config.Config) {
			cfg.Auth.Enabled = true
		}, `unknown provider ""`},
		{"sessions", func(cfg *config.Config) {
			cfg.HTTP.Session.Types = map[string]string{"default": "sqlitestore"}
			cfg.HTTP.Session.Stores = map[string]map[string]string{"sqlitestore": {"enabled": "true"}}
		}, `unknown enabled session store "sqlitestore"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{}
			cfg.Auth.VerificationTokenExpiration, cfg.Auth.ResetTokenExpiration = time.Hour, time.Hour
			tc.configure(&cfg)
			app, err := application.New(t.Context(), cfg, application.Options{})
			if app != nil {
				app.Shutdown()
				t.Fatal("opened application without the configured storage provider")
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}
