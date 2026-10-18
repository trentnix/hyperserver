package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	"github.com/trentnix/hyperserver/pkg/services/session/sqlite"
)

func validSessionConfig() *config.Config {
	c := &config.Config{}
	c.HTTP.Session.JwtKey = strings.Repeat("s", 32)
	c.HTTP.Session.TokenAge = time.Hour
	c.HTTP.Session.CookieAge = time.Hour
	c.HTTP.Session.Stores = map[string]map[string]string{"cookiestore": {"enabled": "true"}}
	c.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	return c
}

func TestSessionSetupRejectsInvalidSelections(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, failure := range []string{"missing", "disabled", "malformed", "duplicate"} {
			t.Run(provider+"/"+failure, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "sessions.db")
				cfg := validSessionConfig()
				cfg.HTTP.Session.Types = map[string]string{"default": provider}
				cfg.HTTP.Session.Stores = map[string]map[string]string{
					provider: {"enabled": "true", "connection": path, "sessiontable": "sessions"},
				}
				switch failure {
				case "missing":
					delete(cfg.HTTP.Session.Stores, provider)
				case "disabled":
					cfg.HTTP.Session.Stores[provider]["enabled"] = "false"
				case "malformed":
					cfg.HTTP.Session.Stores[provider]["enabled"] = "perhaps"
				case "duplicate":
					cfg.HTTP.Session.Stores[strings.ToUpper(provider)] = map[string]string{"enabled": "true"}
				}

				manager, err := NewSessionManager(t.Context(), cfg)
				if manager != nil {
					t.Cleanup(func() { manager.Close() })
				}
				if manager != nil || err == nil {
					t.Fatalf("NewSessionManager = %v, %v, want nil and an error", manager, err)
				}
				if count, err := CleanupSessions(t.Context(), cfg, 10); count != 0 || err == nil {
					t.Fatalf("CleanupSessions = %d, %v, want zero and an error", count, err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("invalid selection created storage: %v", err)
				}
			})
		}
	}
}

func TestSessionSetupRejectsNilConfig(t *testing.T) {
	if err := ValidateSessionConfig(nil); err == nil {
		t.Fatal("validation accepted nil configuration")
	}
	if manager, err := NewSessionManager(t.Context(), nil); manager != nil || err == nil {
		t.Fatalf("NewSessionManager = %v, %v, want nil and an error", manager, err)
	}
	if count, err := CleanupSessions(t.Context(), nil, 10); count != 0 || err == nil {
		t.Fatalf("CleanupSessions = %d, %v, want zero and an error", count, err)
	}
}

func TestSessionSetupSelectsBuiltInProviders(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		t.Run(provider, func(t *testing.T) {
			cfg := validSessionConfig()
			cfg.HTTP.Session.Types["default"] = provider
			cfg.HTTP.Session.Stores["sqliteStore"] = map[string]string{
				"enabled": "true", "connection": filepath.Join(t.TempDir(), "sessions.db"), "sessiontable": "sessions",
			}
			if err := ValidateSessionConfig(cfg); err != nil {
				t.Fatal(err)
			}
			manager, err := NewSessionManager(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { manager.Close() })
			store, err := manager.Store("user")
			if err != nil || store == nil || !store.IsEnabled() {
				t.Fatalf("selected store = %T, %v", store, err)
			}
			switch provider {
			case "cookieStore":
				if _, ok := store.(*cookie.CookieStore); !ok {
					t.Fatalf("selected store = %T, want CookieStore", store)
				}
			case "sqliteStore":
				if _, ok := store.(*sqlite.SQLiteStore); !ok {
					t.Fatalf("selected store = %T, want SQLiteStore", store)
				}
				if count, err := CleanupSessions(t.Context(), cfg, 10); err != nil || count != 0 {
					t.Fatalf("cleanup = %d, %v", count, err)
				}
			}
		})
	}
}
