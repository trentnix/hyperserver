package session_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	sessions "github.com/trentnix/hyperserver/pkg/services/session"
)

func sqliteManagerConfig(path, table string) *config.Config {
	c := validSessionConfig()
	c.HTTP.Session.Stores["sqliteStore"] = map[string]string{"enabled": "true", "connection": path, "sessiontable": table}
	c.HTTP.Session.Types["default"] = "sqliteStore"
	return c
}

func TestSessionManagerIsolationAndReuse(t *testing.T) {
	for _, sameFile := range []bool{false, true} {
		t.Run(fmt.Sprintf("same-file=%t", sameFile), func(t *testing.T) {
			root := t.TempDir()
			firstConfig := sqliteManagerConfig(filepath.Join(root, "first.db"), "first_sessions")
			firstConfig.HTTP.Session.Types["cookie"] = "cookieStore"
			firstConfig.HTTP.Session.Types["alias"] = "SQLITESTORE"
			secondConfig := sqliteManagerConfig(filepath.Join(root, "second.db"), "second_sessions")
			if sameFile {
				secondConfig.HTTP.Session.Stores["sqliteStore"]["connection"] = firstConfig.HTTP.Session.Stores["sqliteStore"]["connection"]
			}
			first, err := sessions.NewConfiguredManager(context.Background(), firstConfig, configuredTestProviders(firstConfig))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { first.Close() })
			second, err := sessions.NewConfiguredManager(context.Background(), secondConfig, configuredTestProviders(secondConfig))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { second.Close() })
			one, err := first.Store("anything")
			if err != nil {
				t.Fatal(err)
			}
			two, err := second.Store("anything")
			if err != nil {
				t.Fatal(err)
			}
			if one == two {
				t.Fatal("independent managers share a store")
			}
			value, request := savedSession(t, one)
			if got, err := two.Get(request, value.Name); err != nil || !got.IsNew {
				t.Fatalf("second store read the first store's session: %+v, %v", got, err)
			}
			// Configuration changes after construction must not redirect the manager
			// or replace its signing key. Aliases reuse the prepared provider.
			firstConfig.HTTP.Session.Types["default"] = "cookieStore"
			firstConfig.HTTP.Session.Stores["sqliteStore"]["connection"] = "/missing/sessions.db"
			firstConfig.HTTP.Session.JwtKey = "changed"
			for _, name := range []string{"anything", "alias"} {
				if got, err := first.Store(name); err != nil || got != one {
					t.Fatal("manager did not reuse its original provider")
				}
			}
			if got, err := first.Store("cookie"); err != nil || got == one {
				t.Fatal("named provider did not override the default")
			}
			if got, err := one.Get(request, value.Name); err != nil || got.ID != value.ID {
				t.Fatal("configuration mutation changed the active signing key")
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := one.Get(request, value.Name); err == nil {
				t.Fatal("manager did not close its provider")
			}
			savedSession(t, two)
			if err := first.Close(); err != nil {
				t.Fatal("repeated Close failed:", err)
			}
		})
	}
}

func TestSessionInitializationFailureAndRetry(t *testing.T) {
	for _, failure := range []string{"open", "schema", "canceled", "deadline"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "sessions.db")
			c := sqliteManagerConfig(path, "sessions")
			ctx := context.Background()
			var want error
			switch failure {
			case "open":
				c.HTTP.Session.Stores["sqliteStore"]["connection"] = filepath.Join(root, "missing", "sessions.db")
			case "schema":
				db, err := database.Setup("sqlite3", path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Close() })
				// Force index creation to fail after the session table was created.
				if _, err := db.Exec("CREATE TABLE sessions_expiration (id TEXT)"); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			case "deadline":
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 0)
				defer cancel()
				want = context.DeadlineExceeded
			}
			manager, err := sessions.NewConfiguredManager(ctx, c, configuredTestProviders(c))
			if manager != nil || err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("failed setup returned %v, %v", manager, err)
			}
			if failure == "schema" {
				db, err := database.Setup("sqlite3", path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				var count int
				if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'sessions'`).Scan(&count); err != nil || count != 0 {
					t.Fatal("failed setup left a partial schema:", err)
				}
				if _, err := db.Exec("DROP TABLE sessions_expiration"); err != nil {
					t.Fatal(err)
				}
			} else if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("failed setup unexpectedly created storage:", err)
			}
			c.HTTP.Session.Stores["sqliteStore"]["connection"] = path
			manager, err = sessions.NewConfiguredManager(context.Background(), c, configuredTestProviders(c))
			if err != nil {
				t.Fatal("retry failed:", err)
			}
			defer manager.Close()
			store, err := manager.Store("test")
			if err != nil {
				t.Fatal(err)
			}
			savedSession(t, store)
		})
	}
}

func TestUnselectedSessionProvidersStayInactive(t *testing.T) {
	c := sqliteManagerConfig(filepath.Join(t.TempDir(), "missing", "sessions.db"), "sessions")
	c.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	m, err := sessions.NewConfiguredManager(context.Background(), c, configuredTestProviders(c))
	if err != nil {
		t.Fatal("unselected SQLite provider was opened:", err)
	}
	defer m.Close()
	if store, err := m.Store("anything"); err != nil || !store.IsEnabled() {
		t.Fatal("manager initialized unselected providers")
	}
	m, err = sessions.NewConfiguredManager(context.Background(), &config.Config{}, configuredTestProviders(&config.Config{}))
	if err != nil {
		t.Fatal("empty session configuration requires providers:", err)
	}
	defer m.Close()
}

func TestSessionSetupResolvesStoragePath(t *testing.T) {
	for _, connection := range []string{"sessions.db?_timeout=25", "file:sessions.db?_timeout=25"} {
		t.Run(connection, func(t *testing.T) {
			cfg := sqliteManagerConfig(connection, "sessions")
			cfg.App.WorkingDirectory = t.TempDir()
			manager, err := sessions.NewConfiguredManager(t.Context(), cfg, configuredTestProviders(cfg))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { manager.Close() })
			store, err := manager.Store("test")
			if err != nil {
				t.Fatal(err)
			}
			savedSession(t, store)
			if _, err := os.Stat(filepath.Join(cfg.App.WorkingDirectory, "sessions.db")); err != nil {
				t.Fatal("storage was not created in the configured working directory:", err)
			}
			if cfg.HTTP.Session.Stores["sqliteStore"]["connection"] != connection {
				t.Fatal("setup changed the configured connection")
			}
		})
	}
}

func savedSession(t *testing.T, store sessions.SessionStore) (*sessions.Session, *http.Request) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	s := sessions.NewSession(store, "test-session")
	s.Data["identity"] = "account"
	w := httptest.NewRecorder()
	if err := s.Save(w, r); err != nil {
		t.Fatal(err)
	}
	r.AddCookie(w.Result().Cookies()[0])
	return s, r
}
