package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/cmd/internal/setup"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestStartupOnlyOpensRequiredSQLPool(t *testing.T) {
	for _, mode := range []string{"auth disabled", "custom accounts", "supplied accounts", "SQLite accounts", "SQLite contacts", "provider failure"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Config{Auth: config.AuthConfig{
				Enabled:                     mode != "auth disabled" && mode != "SQLite contacts",
				JwtKey:                      "optional-database-account-key0000",
				VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
				Services: map[string]map[string]string{"email": {"enabled": "true"}},
			}}
			// Deliberately omit SQL settings, even when authentication is enabled.
			if mode == "custom accounts" || mode == "provider failure" {
				cfg.Auth.AccountStorage.Provider = "custom"
			}
			app, err := newApplication(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := app.Shutdown(); err != nil {
					t.Error(err)
				}
			})
			store := &replacementSessionStore{records: make(map[string]replacementSessionRecord)}
			manager, err := session.NewSessionManager(t.Context(), map[string]string{"default": "custom"}, map[string]session.StoreFactory{
				"custom": func(context.Context) (session.SessionStore, error) { return store, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			app.ownSessions(manager)
			accounts := setup.NewAccountStorage(app.Config)
			opened, closed := false, false
			failure := errors.New("provider failed")
			if err := accounts.RegisterProvider("custom", setup.AccountProvider{Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
				opened = true
				close := func() error { closed = true; return nil }
				if mode == "provider failure" {
					return nil, close, failure
				}
				return &replacementAccountReads{}, close, nil
			}}); err != nil {
				t.Fatal(err)
			}
			if mode == "supplied accounts" {
				app.AccountRepository = &replacementAccountReads{}
			}
			directory := t.TempDir()
			if mode == "SQLite contacts" {
				directory = ""
			}
			server, listener, err := prepareHTTP(t.Context(), app, accounts, directory)
			if listener != nil {
				defer listener.Close()
			}
			if mode == "SQLite accounts" || mode == "SQLite contacts" {
				if err == nil || !strings.Contains(err.Error(), "database") {
					t.Fatalf("required SQL pool did not fail: %v", err)
				}
				return
			}
			if mode == "provider failure" {
				if !errors.Is(err, failure) || !opened || !closed {
					t.Fatalf("provider failure or cleanup lost: %v", err)
				}
				return
			}
			if err != nil || app.Database != nil {
				t.Fatalf("unused SQL pool affected startup: %v", err)
			}
			w := httptest.NewRecorder()
			server.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/contact", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("contact page status %d", w.Code)
			}
			if opened != (mode == "custom accounts") || closed {
				t.Fatal("incorrect provider ownership")
			}
			if err := app.Shutdown(); err != nil {
				t.Fatal(err)
			}
			if closed != opened {
				t.Fatal("custom provider was not closed")
			}
		})
	}
}

func TestApplicationsResolveIndependentStoragePaths(t *testing.T) {
	var apps []*application
	for range 2 {
		cfg := config.Config{
			App:      config.AppConfig{WorkingDirectory: t.TempDir()},
			Database: config.DatabaseConfig{Driver: "sqlite3", Connection: "accounts.db"},
		}
		cfg.HTTP.Session.JwtKey = "independent-storage-test-key0000"
		cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
		cfg.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
		cfg.HTTP.Session.Stores = map[string]map[string]string{"sqliteStore": {
			"enabled": "true", "connection": "sessions.db", "sessiontable": "sessions",
		}}
		app, err := newApplication(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := app.Shutdown(); err != nil {
				t.Error(err)
			}
		})
		if err := app.initializeDatabase(context.Background()); err != nil {
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
