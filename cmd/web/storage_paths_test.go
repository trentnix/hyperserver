package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestApplicationsResolveIndependentStoragePaths(t *testing.T) {
	var apps []*server.ApplicationServer
	for range 2 {
		cfg := config.Config{
			App:      config.AppConfig{WorkingDirectory: t.TempDir()},
			Database: config.DatabaseConfig{Driver: "sqlite3", Connection: "accounts.db"},
		}
		cfg.HTTP.Session.JwtKey = "independent-storage-test-key"
		cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
		cfg.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
		cfg.HTTP.Session.Stores = map[string]map[string]string{"sqliteStore": {
			"enabled": "true", "connection": "sessions.db", "sessiontable": "sessions",
		}}
		app, err := server.NewApplicationServer(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := app.Shutdown(); err != nil {
				t.Error(err)
			}
		})
		if err := app.InitializeDatabase(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := initializeSessions(context.Background(), app); err != nil {
			t.Fatal(err)
		}
		app.Database.SetMaxIdleConns(0)
		apps = append(apps, app)
	}
	t.Chdir(t.TempDir())
	for _, app := range apps {
		var seq int
		var name, path string
		if err := app.Database.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil || path != filepath.Join(app.Config.App.WorkingDirectory, "accounts.db") {
			t.Fatalf("database path=%q error=%v", path, err)
		}
		r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), app.SessionManager)
		value, err := session.New(r, "example")
		if err != nil {
			t.Fatal(err)
		}
		value.Data["owner"] = app.Config.App.WorkingDirectory
		if err := value.Save(httptest.NewRecorder(), r); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(app.Config.App.WorkingDirectory, "sessions.db")); err != nil {
			t.Fatal("session storage did not use its application directory:", err)
		}
	}
	for _, name := range []string{"accounts.db", "sessions.db"} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("storage opened in the process directory: %s %v", name, err)
		}
	}
}
