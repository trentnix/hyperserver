package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

func TestStatelessApplication(t *testing.T) {
	// Construction must not load the developer's file or environment settings.
	t.Setenv("HYPERSERVER_HTTP_PORT", "invalid")
	app, err := NewApplicationServer(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(path, []byte(`{{.Title}}: {{.Data}}`), 0600); err != nil {
		t.Fatal(err)
	}
	app.Web.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		page := content.NewManagedContent(r, app.ContentManager)
		page.Title = "page"
		if page.IsHtmx() {
			page.Title = "fragment"
		}
		page.Data = "<safe>"
		page.AddContent(os.DirFS(filepath.Dir(path)), filepath.Base(path))
		if err := page.Render(w, r); err != nil {
			t.Error(err)
		}
	})
	srv := httptest.NewServer(app.Web)
	defer srv.Close()
	for _, mode := range []string{"page", "fragment"} {
		req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "fragment" {
			req.Header.Set("HX-Request", "true")
		}
		response, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || string(body) != mode+": &lt;safe&gt;" {
			t.Fatalf("response: status=%d body=%q error=%v", response.StatusCode, body, err)
		}
		if len(response.Cookies()) != 0 || response.Header.Get("Vary") != "HX-Request" {
			t.Fatal("stateless rendering set a cookie or lost response variation")
		}
	}
	if app.SessionManager != nil || app.AccountRepository != nil || app.Mail != nil {
		t.Fatal("stateless application activated an optional service")
	}
}

func TestApplicationConstructionReturnsErrors(t *testing.T) {
	cfg := config.Config{}
	cfg.HTTP.HSTSMaxAge = -1
	app, err := NewApplicationServer(cfg)
	if app != nil || err == nil || !strings.Contains(err.Error(), "http.hstsMaxAge") {
		t.Fatalf("invalid configuration: app=%v error=%v", app, err)
	}
}

func TestApplicationsOwnConfiguration(t *testing.T) {
	cfg := config.Config{Auth: config.AuthConfig{Services: map[string]map[string]string{"email": {"enabled": "true"}}}}
	cfg.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	first, err := NewApplicationServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewApplicationServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first.Config.Auth.Services["email"]["enabled"] = "false"
	cfg.HTTP.Session.Types["default"] = "sqliteStore"
	if second.Config.Auth.Services["email"]["enabled"] != "true" || cfg.Auth.Services["email"]["enabled"] != "true" {
		t.Fatal("disabling a provider changed another configuration")
	}
	for _, app := range []*ApplicationServer{first, second} {
		if app.Config.HTTP.Session.Types["default"] != "cookieStore" {
			t.Fatal("source mutation changed application configuration")
		}
	}
}

func TestOptionalServicesInitializeExplicitly(t *testing.T) {
	cfg := config.Config{Database: config.DatabaseConfig{Driver: "unknown"}, Mail: config.MailConfig{Timeout: -time.Second}}
	cfg.HTTP.Session.Types = map[string]string{"default": "unknown"}
	app, err := NewApplicationServer(cfg)
	if err != nil {
		t.Fatal("unused services prevented construction:", err)
	}
	if err := app.InitializeMail(); err == nil || app.Mail != nil {
		t.Fatal("invalid mail initialization succeeded")
	}
	app.Config.Mail.Timeout = 0
	app.Config.Mail.FromAddress = "test@example.invalid"
	if err := app.InitializeMail(); err != nil {
		t.Fatal("mail retry:", err)
	}
	mail := app.Mail
	app.Config.Mail.Timeout = -time.Second
	if err := app.InitializeMail(); err != nil || app.Mail != mail {
		t.Fatal("repeated initialization replaced the client:", err)
	}
	if !errors.Is(mail.ValidateConfig(), messaging.ErrMailUnavailable) {
		t.Fatal("unconfigured mail reported available delivery")
	}
	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
}
