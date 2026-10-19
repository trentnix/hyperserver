package configured_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/services/configured"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

type accounts struct{ user.AccountRepository }
type store struct {
	session.SessionStore
	closes int
}

func (*store) IsEnabled() bool { return true }
func (s *store) Close() error  { s.closes++; return nil }

func TestAccountsSelectOnlyConfiguredProvider(t *testing.T) {
	for _, name := range []string{"custom", "sqlite", "unknown", "disabled"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{
				App:      config.AppConfig{WorkingDirectory: t.TempDir()},
				Database: config.DatabaseConfig{Driver: "not-sqlite", Connection: "must-not-open.db"},
				Auth:     config.AuthConfig{Enabled: name != "disabled", AccountStorage: config.AccountStorageConfig{Provider: name}},
			}
			opens, closes := 0, 0
			factory := user.StorageProvider{Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
				opens++
				return &accounts{}, func() error { closes++; return nil }, nil
			}}
			// Replacing the built-in name must also avoid its SQL factory.
			repository, cleanup, err := configured.OpenAccounts(t.Context(), cfg, map[string]user.StorageProvider{"custom": factory, "sqlite": factory})
			if name == "unknown" {
				if err == nil || repository != nil || cleanup != nil {
					t.Fatal("unknown provider accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			want := 0
			if name == "custom" || name == "sqlite" {
				want = 1
				if repository == nil || cleanup == nil {
					t.Fatal("missing storage")
				}
				if closes != 0 {
					t.Fatal("closed successful open")
				}
				if err := cleanup(); err != nil {
					t.Fatal(err)
				}
			}
			if opens != want || closes != want {
				t.Fatalf("opens=%d closes=%d", opens, closes)
			}
			entries, err := os.ReadDir(cfg.App.WorkingDirectory)
			if err != nil || len(entries) != 0 {
				t.Fatalf("unselected provider wrote files: %v %v", entries, err)
			}
		})
	}
}

func TestOwnedAccountsPersistenceAndCleanup(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{WorkingDirectory: t.TempDir()},
		Auth: config.AuthConfig{Enabled: true, AccountStorage: config.AccountStorageConfig{
			Options: map[string]string{"connection": "accounts.db"},
		}},
	}
	repository, cleanup, err := configured.OpenAccounts(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup() })
	if cfg.Auth.AccountStorage.Provider != "" {
		t.Fatal("default selection changed caller configuration")
	}
	u := &user.User{Email: "reader@example.com", Password: "stored-password-hash", RegistrationAuthType: "email"}
	if err := repository.Create(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetByID(t.Context(), u.ID); err == nil {
		t.Fatal("repository remained open after cleanup")
	}
	if _, err := os.Stat(filepath.Join(cfg.App.WorkingDirectory, "accounts.db")); err != nil {
		t.Fatal(err)
	}
	reopened, closeReopened, err := configured.OpenAccounts(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReopened()
	if got, err := reopened.GetByEmail(t.Context(), "READER@example.com"); err != nil || got.ID != u.ID {
		t.Fatalf("reopened account: %v %v", got, err)
	}
}

func TestAccountFailureClosesPartialResources(t *testing.T) {
	cause, closeErr := errors.New("open failed"), errors.New("close failed")
	closes := 0
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true, AccountStorage: config.AccountStorageConfig{Provider: "custom"}}}
	repository, cleanup, err := configured.OpenAccounts(t.Context(), cfg, map[string]user.StorageProvider{"custom": {
		Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
			return nil, func() error { closes++; return closeErr }, cause
		},
	}})
	if repository != nil || cleanup != nil || closes != 1 || !errors.Is(err, cause) || !errors.Is(err, closeErr) {
		t.Fatalf("failed open: closes=%d err=%v", closes, err)
	}
}

func TestAccountsWithSharedPoolKeepCallerOwnership(t *testing.T) {
	pool, err := dbsqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true}}
	repository, cleanup, err := configured.OpenAccounts(t.Context(), cfg, map[string]user.StorageProvider{
		"sqlite": usersqlite.ConfiguredProvider(*cfg, pool),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil {
		t.Cleanup(func() { cleanup() })
		t.Fatal("setup took ownership of the shared pool")
	}
	if err := repository.Create(t.Context(), &user.User{
		Email: "shared@example.com", Password: "stored-password-hash", RegistrationAuthType: "email",
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.PingContext(t.Context()); err != nil {
		t.Fatal("setup closed the caller's pool", err)
	}
}

func TestSessionsSelectConfiguredProvider(t *testing.T) {
	for _, name := range []string{"cookiestore", "sqlitestore", "custom", "unknown"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{App: config.AppConfig{WorkingDirectory: t.TempDir()}}
			cfg.HTTP.Session.JwtKey = strings.Repeat("a", 64)
			cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
			cfg.HTTP.Session.Types = map[string]string{"default": name}
			cfg.HTTP.Session.Stores = map[string]map[string]string{name: {"enabled": "true"}}
			if name == "sqlitestore" {
				cfg.HTTP.Session.Stores[name]["connection"] = "sessions.db"
				cfg.HTTP.Session.Stores[name]["sessiontable"] = "sessions"
			}
			custom := &store{}
			opens := 0
			manager, err := configured.NewSessionManager(t.Context(), cfg, map[string]session.ConfiguredProvider{"custom": {
				Open: func(context.Context, map[string]string) (session.SessionStore, error) { opens++; return custom, nil },
			}})
			if name == "unknown" {
				if err == nil || manager != nil {
					t.Fatal("unknown store accepted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := manager.Close(); err != nil {
					t.Fatal(err)
				}
			}
			want := 0
			if name == "custom" {
				want = 1
			}
			if opens != want || custom.closes != want {
				t.Fatalf("opens=%d closes=%d", opens, custom.closes)
			}
			_, err = os.Stat(filepath.Join(cfg.App.WorkingDirectory, "sessions.db"))
			if name == "sqlitestore" && err != nil {
				t.Fatal(err)
			}
			if name != "sqlitestore" && !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unselected SQLite store wrote file", err)
			}
		})
	}
}

func TestConfiguredStorageRejectsNilConfigAndCancellation(t *testing.T) {
	if _, _, err := configured.OpenAccounts(t.Context(), nil, nil); err == nil {
		t.Fatal("nil config accepted")
	}
	if _, err := configured.NewSessionManager(t.Context(), nil, nil); err == nil {
		t.Fatal("nil config accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true, AccountStorage: config.AccountStorageConfig{Provider: "sqlite"}}}
	if _, _, err := configured.OpenAccounts(ctx, cfg, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := configured.NewSessionManager(ctx, cfg, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSessionProviderCatalogIsIndependent(t *testing.T) {
	cfg := config.Config{}
	cfg.HTTP.Session.JwtKey = strings.Repeat("a", 64)
	cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
	first := configured.SessionProviders(cfg)
	second := configured.SessionProviders(cfg)
	delete(first, "sqlitestore")
	cfg.HTTP.Session.JwtKey = "invalid"
	if second["sqlitestore"].Open == nil || second["sqlitestore"].OpenMaintenance == nil {
		t.Fatal("catalog entries were shared or maintenance was lost")
	}
	if err := second["cookiestore"].Validate(nil); err != nil {
		t.Fatal("factory configuration was not snapshotted", err)
	}
}

func TestSessionsReplaceBuiltInProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.HTTP.Session.Types = map[string]string{"default": "sqlitestore", "identity": "sqlitestore"}
	cfg.HTTP.Session.Stores = map[string]map[string]string{"sqlitestore": {"enabled": "true", "custom": "original"}}
	custom := &store{}
	opens := 0
	providers := map[string]session.ConfiguredProvider{"sqlitestore": {
		Open: func(_ context.Context, options map[string]string) (session.SessionStore, error) {
			opens++
			if options["custom"] != "original" {
				t.Fatal("provider did not receive configured options")
			}
			options["custom"] = "changed"
			return custom, nil
		},
	}}
	// No SQLite connection or signing keys: replacement must bypass its validation.
	manager, err := configured.NewSessionManager(t.Context(), cfg, providers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	for _, name := range []string{"default", "identity"} {
		got, err := manager.Store(name)
		if err != nil || got != custom {
			t.Fatalf("store %q = %v, %v", name, got, err)
		}
	}
	if cfg.HTTP.Session.Stores["sqlitestore"]["custom"] != "original" || opens != 1 {
		t.Fatalf("configuration changed or provider opened more than once: opens=%d", opens)
	}
	if err := manager.Close(); err != nil || custom.closes != 1 {
		t.Fatalf("cleanup: closes=%d err=%v", custom.closes, err)
	}
}

func TestSessionsWithoutSelectionsOpenNothing(t *testing.T) {
	cfg := &config.Config{App: config.AppConfig{WorkingDirectory: t.TempDir()}}
	manager, err := configured.NewSessionManager(t.Context(), cfg, map[string]session.ConfiguredProvider{"unused": {
		Open: func(context.Context, map[string]string) (session.SessionStore, error) {
			t.Fatal("unselected factory ran")
			return nil, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(cfg.App.WorkingDirectory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unselected stores wrote files: %v %v", entries, err)
	}
}

func TestSessionFailureClosesOpenedStores(t *testing.T) {
	cause := errors.New("store failed")
	first := &store{}
	cfg := &config.Config{}
	cfg.HTTP.Session.Types = map[string]string{"default": "first", "identity": "second"}
	cfg.HTTP.Session.Stores = map[string]map[string]string{
		"first": {"enabled": "true"}, "second": {"enabled": "true"},
	}
	manager, err := configured.NewSessionManager(t.Context(), cfg, map[string]session.ConfiguredProvider{
		"first": {Open: func(context.Context, map[string]string) (session.SessionStore, error) {
			return first, nil
		}},
		"second": {Open: func(context.Context, map[string]string) (session.SessionStore, error) {
			return nil, cause
		}},
	})
	if manager != nil || !errors.Is(err, cause) || first.closes != 1 {
		t.Fatalf("failed setup: manager=%v err=%v closes=%d", manager, err, first.closes)
	}
}
