package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestCleanupAccountTokensWithoutApplication(t *testing.T) {
	for _, scenario := range []string{"disabled", "invalid limit", "canceled", "unknown provider", "missing storage", "explicit SQLite", "unsupported driver", "unsupported options"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounts.db")
			cfg := &config.Config{
				Auth:     config.AuthConfig{Enabled: true},
				Database: config.DatabaseConfig{Driver: "sqlite3", Connection: path},
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
			case "unsupported driver":
				cfg.Database.Driver = "other"
			case "unsupported options":
				cfg.Auth.AccountStorage.Options = map[string]string{"connection": "unsupported"}
			}

			count, err := CleanupAccountTokens(ctx, cfg, limit)
			if count != 0 || (err != nil) != (scenario != "disabled") {
				t.Fatalf("cleanup: count=%d, error=%v", count, err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation:", err)
			}
			if scenario == "unknown provider" && !strings.Contains(err.Error(), "unknown provider") {
				t.Fatal("configuration did not select the provider:", err)
			}
			if scenario == "unsupported driver" && !strings.Contains(err.Error(), "requires the sqlite3 database driver") {
				t.Fatal("configuration did not reject the driver:", err)
			}
			if scenario == "unsupported options" && !strings.Contains(err.Error(), "accepts no options") {
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
		Auth:     config.AuthConfig{Enabled: true},
		Database: config.DatabaseConfig{Driver: "sqlite3", Connection: "maintenance.db?_timeout=25"},
	}
	cfg.App.WorkingDirectory = directory
	if _, _, err := NewAccountStorage(cfg).Open(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE sessions (id TEXT PRIMARY KEY, expires_at TIMESTAMP)"); err != nil {
		t.Fatal(err)
	}
	cfg.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
	cfg.HTTP.Session.Stores = map[string]map[string]string{
		"sqliteStore": {"enabled": "true", "connection": "maintenance.db?_timeout=25", "sessiontable": "sessions"},
	}

	for name, cleanup := range map[string]func(context.Context, *config.Config, int) (int64, error){
		"accounts": CleanupAccountTokens,
		"sessions": CleanupSessions,
	} {
		t.Run(name, func(t *testing.T) {
			if count, err := cleanup(t.Context(), cfg, 1); err != nil || count != 0 {
				t.Fatalf("maintenance did not open configured storage: %d, %v", count, err)
			}
		})
	}
	if cfg.Database.Connection != "maintenance.db?_timeout=25" || cfg.HTTP.Session.Stores["sqliteStore"]["connection"] != "maintenance.db?_timeout=25" {
		t.Fatal("maintenance rewrote caller configuration")
	}
}

func accountProviderApplication(name string) *AccountStorage {
	return NewAccountStorage(&config.Config{Auth: config.AuthConfig{
		Enabled: true, AccountStorage: config.AccountStorageConfig{Provider: name},
	}})
}

type tokenMaintenanceFunc func(context.Context, int) (int64, error)

func (f tokenMaintenanceFunc) DeleteExpiredTokens(ctx context.Context, limit int) (int64, error) {
	return f(ctx, limit)
}

func TestAccountMaintenanceLifecycle(t *testing.T) {
	openErr, deleteErr, closeErr := errors.New("open failed"), errors.New("delete failed"), errors.New("close failed")
	for _, scenario := range []string{"success", "open failure", "delete failure", "close failure", "nil maintenance", "canceled before open", "canceled after open", "unsupported", "disabled", "invalid limit"} {
		t.Run(scenario, func(t *testing.T) {
			s := accountProviderApplication("custom")
			s.config.Auth.AccountStorage.Options = map[string]string{"connection": "custom-storage"}
			type contextKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "maintenance"))
			defer cancel()
			opens, deletes, closes := 0, 0, 0
			provider := AccountProvider{
				Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
					t.Fatal("maintenance initialized runtime storage")
					return nil, nil, nil
				},
				OpenMaintenance: func(got context.Context, options map[string]string) (user.TokenMaintenance, func() error, error) {
					opens++
					if got != ctx || options["connection"] != "custom-storage" {
						t.Fatal("maintenance lost context or configuration")
					}
					options["connection"] = "provider-modified"
					closeStore := func() error {
						closes++
						if scenario == "close failure" || scenario == "open failure" || scenario == "delete failure" {
							return closeErr
						}
						return nil
					}
					if scenario == "open failure" {
						return nil, closeStore, openErr
					}
					if scenario == "nil maintenance" {
						return nil, closeStore, nil
					}
					if scenario == "canceled after open" {
						cancel()
					}
					return tokenMaintenanceFunc(func(got context.Context, limit int) (int64, error) {
						deletes++
						if got != ctx || limit != 5 {
							t.Fatal("maintenance lost context or batch limit")
						}
						if scenario == "delete failure" {
							return 2, deleteErr
						}
						return 3, nil
					}), closeStore, nil
				},
			}
			limit := 5
			switch scenario {
			case "canceled before open":
				cancel()
			case "unsupported":
				provider.OpenMaintenance = nil
			case "disabled":
				s.config.Auth.Enabled = false
			case "invalid limit":
				limit = 0
			}
			if err := s.RegisterProvider("custom", provider); err != nil {
				t.Fatal(err)
			}

			count, err := s.CleanupTokens(ctx, limit)
			wantOpen, wantDelete, wantCount := 1, 0, int64(0)
			switch scenario {
			case "success", "close failure":
				wantDelete, wantCount = 1, 3
			case "delete failure":
				wantDelete, wantCount = 1, 2
			case "canceled before open", "unsupported", "disabled", "invalid limit":
				wantOpen = 0
			}
			if opens != wantOpen || closes != wantOpen || deletes != wantDelete || count != wantCount {
				t.Fatalf("maintenance: opens=%d, closes=%d, deletes=%d, count=%d, error=%v", opens, closes, deletes, count, err)
			}
			wantError := scenario != "success" && scenario != "disabled"
			if (err != nil) != wantError {
				t.Fatalf("maintenance error = %v", err)
			}
			for expected, applies := range map[error]bool{
				openErr: scenario == "open failure", deleteErr: scenario == "delete failure",
				closeErr:         scenario == "open failure" || scenario == "delete failure" || scenario == "close failure",
				context.Canceled: scenario == "canceled before open" || scenario == "canceled after open",
			} {
				if applies && !errors.Is(err, expected) {
					t.Fatalf("lost error %v: %v", expected, err)
				}
			}
			if s.config.Auth.AccountStorage.Options["connection"] != "custom-storage" {
				t.Fatal("maintenance changed configuration")
			}
		})
	}
}

func TestAccountMaintenanceRetry(t *testing.T) {
	s := accountProviderApplication("custom")
	opens, closes := 0, 0
	openErr := errors.New("temporary open failure")
	if err := s.RegisterProvider("custom", AccountProvider{
		Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
			t.Fatal("maintenance called runtime initialization")
			return nil, nil, nil
		},
		OpenMaintenance: func(context.Context, map[string]string) (user.TokenMaintenance, func() error, error) {
			opens++
			if opens == 1 {
				return nil, func() error { closes++; return nil }, openErr
			}
			// Providers that borrow resources can omit a close function.
			return tokenMaintenanceFunc(func(context.Context, int) (int64, error) { return 1, nil }), nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if count, err := s.CleanupTokens(context.Background(), 1); count != 0 || !errors.Is(err, openErr) || closes != 1 {
		t.Fatalf("failed attempt: count=%d, error=%v, closes=%d", count, err, closes)
	}
	for range 2 {
		if count, err := s.CleanupTokens(context.Background(), 1); count != 1 || err != nil {
			t.Fatalf("subsequent batch: count=%d, error=%v", count, err)
		}
	}
	if opens != 3 || closes != 1 {
		t.Fatalf("maintenance retained stale state: opens=%d, closes=%d", opens, closes)
	}
}

func TestAccountMaintenanceDeadline(t *testing.T) {
	for _, stage := range []string{"open", "delete"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := accountProviderApplication("custom")
				closes, deletes := 0, 0
				if err := s.RegisterProvider("custom", AccountProvider{
					Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
						t.Fatal("maintenance initialized runtime storage")
						return nil, nil, nil
					},
					OpenMaintenance: func(ctx context.Context, _ map[string]string) (user.TokenMaintenance, func() error, error) {
						if stage == "open" {
							<-ctx.Done()
						}
						return tokenMaintenanceFunc(func(ctx context.Context, _ int) (int64, error) {
							deletes++
							<-ctx.Done()
							return 0, ctx.Err()
						}), func() error { closes++; return nil }, nil
					},
				}); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				start := time.Now()
				count, err := s.CleanupTokens(ctx, 1)
				wantDeletes := 0
				if stage == "delete" {
					wantDeletes = 1
				}
				if count != 0 || !errors.Is(err, context.DeadlineExceeded) || closes != 1 || deletes != wantDeletes || time.Since(start) != time.Second {
					t.Fatalf("deadline: count=%d, error=%v, closes=%d, deletes=%d, elapsed=%s", count, err, closes, deletes, time.Since(start))
				}
			})
		})
	}
}
