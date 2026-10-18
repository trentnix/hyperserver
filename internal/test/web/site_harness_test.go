package web_test

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/auth"
	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	email "github.com/trentnix/hyperserver/auth/modules/email"
	"github.com/trentnix/hyperserver/config"
	site "github.com/trentnix/hyperserver/modules/site"
	"github.com/trentnix/hyperserver/modules/site/storage"
	"github.com/trentnix/hyperserver/pkg/components/content"
	sqlitedb "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	sessionsqlite "github.com/trentnix/hyperserver/pkg/services/session/sqlite"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

// Site scenarios exercise real forms, layouts, and assets through public APIs.
// Command startup, provider selection, and middleware ordering stay in cmd/web's tests.
func runSiteScenario(t *testing.T, scenario func(*httpHarness), configure ...func(*config.Config)) {
	t.Helper()
	scenario(newSiteHTTPHarness(t, nil, configure...))
}

func runSiteScenarioWithCatalog(t *testing.T, catalog func(*httpHarness) ([]handlers.Descriptor, error), scenario func(*httpHarness), configure ...func(*config.Config)) {
	t.Helper()
	scenario(newSiteHTTPHarness(t, catalog, configure...))
}

func newSiteHTTPHarness(t testing.TB, catalogFactory func(*httpHarness) ([]handlers.Descriptor, error), configure ...func(*config.Config)) *httpHarness {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		App:      config.AppConfig{Name: "HTTP test application", WorkingDirectory: root, RenderNotifications: true},
		HTTP:     config.HTTPConfig{ListenHost: "127.0.0.1", Port: 8080},
		Database: config.DatabaseConfig{Driver: "sqlite3", Connection: filepath.Join(t.TempDir(), "application.db")},
		Auth: config.AuthConfig{
			Enabled: true, JwtKey: "http-test-only-auth-signing-key0",
			RegistrationEnabled: true, RegisterRequiresVerification: true,
			VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
			Services: map[string]map[string]string{"email": {"enabled": "true"}},
		},
	}
	cfg.HTTP.Session.JwtKey = "http-test-only-session-signing-key"
	cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
	cfg.HTTP.Session.Stores = map[string]map[string]string{
		"cookieStore": {"enabled": "true"},
		"sqliteStore": {"enabled": "true", "connection": cfg.Database.Connection, "sessiontable": "session"},
	}
	cfg.HTTP.Session.Types = map[string]string{"default": "cookieStore", "auth-user-session": "sqliteStore"}
	for _, change := range configure {
		change(&cfg)
	}
	app, err := server.NewApplicationServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})

	var db *sql.DB
	if cfg.Database.Driver == "sqlite3" {
		db, err = sqlitedb.Open(cfg.Database.Connection)
	} else {
		// Timing tests supply an instrumented SQLite driver.
		db, err = sql.Open(cfg.Database.Driver, cfg.Database.Connection)
	}
	if err != nil {
		t.Fatal(err)
	}
	app.OnShutdown("accounts", func(context.Context) error { return db.Close() })
	accounts := usersqlite.NewAccountRepository(db)
	if err := accounts.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	sessions, err := session.NewConfiguredManager(t.Context(), app.Config, map[string]session.ConfiguredProvider{
		"cookieStore": cookie.ConfiguredProvider(*app.Config),
		"sqliteStore": sessionsqlite.ConfiguredProvider(*app.Config),
	})
	if err != nil {
		t.Fatal(err)
	}
	app.OnShutdown("sessions", func(context.Context) error { return sessions.Close() })
	sender := &fakeMailSender{}
	mail, err := messaging.NewMailClientWithSender("test@example.invalid", sender)
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	h := &httpHarness{baseURL: "http://127.0.0.1:8080", app: app, db: db, accounts: accounts, sessions: sessions, mailClient: mail, mail: sender, logs: &capturedLogs{}, cookies: jar}

	if err := site.ConfigureRendering(app.ContentManager, cfg.Auth.Enabled); err != nil {
		t.Fatal(err)
	}
	if cfg.App.RenderNotifications {
		app.ContentManager.LoadNotifications = flash.LoadNotifications
	}
	if catalogFactory == nil {
		catalogFactory = func(h *httpHarness) ([]handlers.Descriptor, error) { return siteCatalog(h, "") }
	}
	catalog, err := catalogFactory(h)
	if err != nil {
		t.Fatal(err)
	}
	log := newTestLogger(h.logs)
	routes, err := routing.NewConfiguredRoutes(app.Web, cfg.HTTP, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := handlers.Setup(logger.Set(t.Context(), log), app, routes, catalog, nil); err != nil {
		t.Fatal(err)
	}
	h.handler, err = siteHTTPHandler(h, log)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func siteCatalog(h *httpHarness, directory string) ([]handlers.Descriptor, error) {
	contacts := func(ctx context.Context) (site.ContactRepository, error) {
		if directory != "" {
			return storage.NewFileContactRepository(ctx, directory)
		}
		return storage.NewSQLiteContactRepository(ctx, h.db)
	}
	return []handlers.Descriptor{
		site.Module(contacts, h.mailClient),
		auth.Module(h.accounts, h.sessions, []auth.Descriptor{email.Provider(h.accounts, h.sessions, h.mailClient)}),
	}, nil
}

const referenceContentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; img-src 'self'; connect-src 'self'; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'"

func siteHTTPHandler(h *httpHarness, log *slog.Logger) (http.Handler, error) {
	loadSession := session.WithSessionManager(h.sessions)
	if h.app.Config.Auth.Enabled {
		loadSession = authmiddleware.LoadSessionManagement(h.accounts, h.sessions)
	}
	handler := http.NewCrossOriginProtection().Handler(loadSession(h.app.Web))
	handler, err := middleware.FromConfig(handler, h.app.Config.HTTP)
	if err != nil {
		return nil, err
	}
	handler = middleware.SecurityHeaders(referenceContentSecurityPolicy)(handler)
	return middleware.LoggerMiddleware(log)(handler), nil
}

func testTemplateFile(t *testing.T, name string) content.TemplatePath {
	t.Helper()
	if !filepath.IsAbs(name) {
		name = filepath.Join("../../..", name)
	}
	name, err := filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}
	return content.TemplatePath{FS: os.DirFS(filepath.Dir(name)), Path: filepath.Base(name)}
}
