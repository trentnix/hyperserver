package server

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
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
		page.AddContent(content.TemplatePath(path))
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
	if app.Database != nil || app.SessionManager != nil || app.AccountRepository != nil || app.Mail != nil {
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
	if err := app.InitializeDatabase(context.Background()); err == nil || app.Database != nil {
		t.Fatal("invalid database initialization succeeded")
	}
	if err := app.InitializeSessions(context.Background()); err == nil || app.SessionManager != nil {
		t.Fatal("invalid session initialization succeeded")
	}
	if err := app.InitializeMail(); err == nil || app.Mail != nil {
		t.Fatal("invalid mail initialization succeeded")
	}
	app.Config.Database = config.DatabaseConfig{Driver: "sqlite3", Connection: ":memory:"}
	if err := app.InitializeDatabase(context.Background()); err != nil {
		t.Fatal("database retry:", err)
	}
	db := app.Database
	app.Config.Database.Driver = "unused-after-initialization"
	if err := app.InitializeDatabase(context.Background()); err != nil || app.Database != db {
		t.Fatal("repeated initialization replaced the pool:", err)
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
	if err := db.Ping(); err == nil {
		t.Fatal("shutdown left the application-owned pool open")
	}
}

// A test driver exposes deadlines and connection cleanup without a remote database.
type startupDriver struct{ connection *startupConnection }

func (d startupDriver) Open(string) (driver.Conn, error) { return d.connection, nil }

type startupConnection struct {
	ping   func(context.Context) error
	closed bool
}

func (c *startupConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *startupConnection) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (c *startupConnection) Ping(ctx context.Context) error      { return c.ping(ctx) }
func (c *startupConnection) Close() error {
	c.closed = true
	return nil
}

func TestDatabaseInitializationCleanup(t *testing.T) {
	want := errors.New("database unavailable")
	conn := &startupConnection{ping: func(ctx context.Context) error {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
			t.Error("database setup has no bounded deadline")
		}
		return want
	}}
	name := t.TempDir()
	sql.Register(name, startupDriver{conn})
	app, err := NewApplicationServer(config.Config{Database: config.DatabaseConfig{Driver: name}})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.InitializeDatabase(context.Background()); !errors.Is(err, want) || app.Database != nil || !conn.closed {
		t.Fatalf("failed setup: error=%v pool=%v closed=%t", err, app.Database, conn.closed)
	}
}

func TestDatabaseCancellationAfterPing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := &startupConnection{ping: func(context.Context) error {
		cancel()
		return nil
	}}
	name := t.TempDir()
	sql.Register(name, startupDriver{conn})
	app, err := NewApplicationServer(config.Config{Database: config.DatabaseConfig{Driver: name}})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.InitializeDatabase(ctx); !errors.Is(err, context.Canceled) || !conn.closed || app.Database != nil {
		t.Fatalf("cancellation after ping: error=%v closed=%t pool=%v", err, conn.closed, app.Database)
	}
}

func TestDatabaseInitializationCancellation(t *testing.T) {
	for _, duration := range []time.Duration{0, time.Second, 10 * time.Second} {
		t.Run(duration.String(), func(t *testing.T) {
			name := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				conn := &startupConnection{ping: func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				}}
				sql.Register(name, startupDriver{conn})
				app, err := NewApplicationServer(config.Config{Database: config.DatabaseConfig{Driver: name}})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), duration)
				defer cancel()
				if duration == 10*time.Second {
					ctx = context.Background() // Exercise the initializer's own deadline.
				}
				started := time.Now()
				err = app.InitializeDatabase(ctx)
				if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) != duration || app.Database != nil {
					t.Fatalf("canceled setup: duration=%s error=%v pool=%v", time.Since(started), err, app.Database)
				}
				if conn.closed != (duration > 0) {
					t.Fatal("setup opened a canceled connection or failed to close an acquired connection")
				}
			})
		})
	}
}
