package configured_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/configured"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

func TestCleanupAccountTokensWithoutApplication(t *testing.T) {
	for _, scenario := range []string{"disabled", "invalid limit", "canceled", "unknown provider", "missing storage", "explicit SQLite", "missing connection", "unsupported options"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounts.db")
			cfg := &config.Config{
				Auth: config.AuthConfig{Enabled: true, AccountStorage: config.AccountStorageConfig{
					Options: map[string]string{"connection": path},
				}},
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			limit := 1
			switch scenario {
			case "disabled":
				cfg.Auth.Enabled = false
				cfg.Auth.AccountStorage.Provider = "unknown"
			case "invalid limit":
				limit = 0
			case "canceled":
				cancel()
			case "unknown provider":
				cfg.Auth.AccountStorage.Provider = "unknown"
			case "explicit SQLite":
				cfg.Auth.AccountStorage.Provider = "sqlite"
			case "missing connection":
				cfg.Auth.AccountStorage.Options = nil
			case "unsupported options":
				cfg.Auth.AccountStorage.Options["typo"] = "unsupported"
			}

			count, err := configured.CleanupAccountTokens(ctx, cfg, limit, nil)
			if count != 0 || (err != nil) != (scenario != "disabled") {
				t.Fatalf("cleanup: count=%d, error=%v", count, err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation:", err)
			}
			if scenario == "unknown provider" && !strings.Contains(err.Error(), "unknown provider") {
				t.Fatal("configuration did not select the provider:", err)
			}
			if scenario == "missing connection" && !strings.Contains(err.Error(), "connection is required") {
				t.Fatal("configuration did not require a connection:", err)
			}
			if scenario == "unsupported options" && !strings.Contains(err.Error(), "accepts only connection") {
				t.Fatal("configuration did not reject unsupported options:", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("maintenance created storage: %v", err)
			}
		})
	}
}

func TestMaintenanceResolvesConfiguredPaths(t *testing.T) {
	directory := t.TempDir()
	connection := filepath.Join(directory, "maintenance.db")
	db, err := database.Setup("sqlite3", connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := &config.Config{
		Auth: config.AuthConfig{Enabled: true, AccountStorage: config.AccountStorageConfig{
			Options: map[string]string{"connection": "maintenance.db?_timeout=25"},
		}},
	}
	cfg.App.WorkingDirectory = directory
	if err := usersqlite.NewAccountRepository(db).Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE sessions (id TEXT PRIMARY KEY, expires_at TIMESTAMP)"); err != nil {
		t.Fatal(err)
	}
	cfg.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
	cfg.HTTP.Session.Stores = map[string]map[string]string{
		"sqliteStore": {"enabled": "true", "connection": "maintenance.db?_timeout=25", "sessiontable": "sessions"},
	}

	for name, cleanup := range map[string]func() (int64, error){
		"accounts": func() (int64, error) { return configured.CleanupAccountTokens(t.Context(), cfg, 1, nil) },
		"sessions": func() (int64, error) { return configured.CleanupSessions(t.Context(), cfg, 1, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			if count, err := cleanup(); err != nil || count != 0 {
				t.Fatalf("maintenance did not open configured storage: %d, %v", count, err)
			}
		})
	}
	if cfg.Auth.AccountStorage.Options["connection"] != "maintenance.db?_timeout=25" || cfg.HTTP.Session.Stores["sqliteStore"]["connection"] != "maintenance.db?_timeout=25" {
		t.Fatal("maintenance rewrote caller configuration")
	}
}
