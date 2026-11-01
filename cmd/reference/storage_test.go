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
	framework "github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestFileContactsDoNotRequireSQL(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		cfg := config.Config{Auth: config.AuthConfig{
			Enabled: authenticated, JwtKey: "optional-database-account-key0000",
			VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
			Services: map[string]map[string]string{"email": {"enabled": "true"}},
		}}
		cfg.HTTP.ListenHost = "127.0.0.1"
		store := &replacementSessionStore{records: make(map[string]replacementSessionRecord)}
		manager, err := session.NewSessionManager(t.Context(), map[string]string{"default": "custom"}, map[string]session.StoreFactory{
			"custom": func(context.Context) (session.SessionStore, error) { return store, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { manager.Close() })
		options := framework.Options{Services: framework.Services{Sessions: manager}}
		if authenticated {
			options.Services.Accounts = &replacementAccountReads{}
		}
		app, err := newApplication(t.Context(), cfg, t.TempDir(), options)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { app.Shutdown() })
		if app.contactDB != nil {
			t.Fatal("file contacts opened SQL storage")
		}
		w := httptest.NewRecorder()
		app.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/contact", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("contact page status=%d", w.Code)
		}
	}
}

func TestApplicationsResolveIndependentStoragePaths(t *testing.T) {
	var apps []*application
	for range 2 {
		cfg := config.Config{
			HTTP:     config.HTTPConfig{ListenHost: "127.0.0.1"},
			App:      config.AppConfig{WorkingDirectory: t.TempDir()},
			Database: config.DatabaseConfig{Driver: "sqlite3", Connection: "accounts.db"},
		}
		cfg.HTTP.Session.JwtKey = "independent-storage-test-key0000"
		cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
		cfg.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
		cfg.HTTP.Session.Stores = map[string]map[string]string{"sqliteStore": {
			"enabled": "true", "connection": "sessions.db", "sessiontable": "sessions",
		}}
		app, err := newApplication(t.Context(), cfg, "", framework.Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := app.Shutdown(); err != nil {
				t.Error(err)
			}
		})
		app.contactDB.SetMaxIdleConns(0)
		apps = append(apps, app)
	}
	t.Chdir(t.TempDir())
	for _, app := range apps {
		var seq int
		var name, path string
		if err := app.contactDB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil || path != filepath.Join(app.Config.App.WorkingDirectory, "accounts.db") {
			t.Fatalf("database path=%q error=%v", path, err)
		}
		r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), app.Sessions)
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
