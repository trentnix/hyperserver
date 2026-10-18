package session_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	"github.com/trentnix/hyperserver/pkg/services/session/sqlite"
)

type configuredStore struct {
	session.SessionStore
	closed int
}

func (*configuredStore) IsEnabled() bool { return true }
func (s *configuredStore) Close() error  { s.closed++; return nil }

type configuredMaintenance struct{}

func (*configuredMaintenance) DeleteExpired(context.Context, int) (int64, error) { return 2, nil }

func configuredSettings() *config.Config {
	cfg := &config.Config{}
	// Custom providers need not use the built-in JWT and cookie settings.
	cfg.HTTP.Session.Stores = map[string]map[string]string{"custom": {"enabled": "true", "location": "original"}}
	cfg.HTTP.Session.Types = map[string]string{"default": "CUSTOM", "another": "custom"}
	return cfg
}

func TestConfiguredProviderSelectionAndOwnership(t *testing.T) {
	cfg := configuredSettings()
	store := &configuredStore{}
	opens, maintenanceCloses := 0, 0
	providers := map[string]session.ConfiguredProvider{
		"Custom": {
			Validate: func(options map[string]string) error {
				options["location"] = "validation copy"
				return nil
			},
			Open: func(ctx context.Context, options map[string]string) (session.SessionStore, error) {
				opens++
				if options["location"] != "original" {
					t.Fatal("validation changed options")
				}
				options["location"] = "open copy"
				return store, nil
			},
			OpenMaintenance: func(ctx context.Context, options map[string]string) (session.ExpiredSessionMaintenance, func() error, error) {
				if options["location"] != "original" {
					t.Fatal("opening changed options")
				}
				options["location"] = "maintenance copy"
				return &configuredMaintenance{}, func() error { maintenanceCloses++; return nil }, nil
			},
		},
		"unused": {Open: func(context.Context, map[string]string) (session.SessionStore, error) {
			t.Fatal("unselected provider opened")
			return nil, nil
		}},
	}
	manager, err := session.NewConfiguredManager(t.Context(), cfg, providers)
	if err != nil {
		t.Fatal(err)
	}
	if opens != 1 || store.closed != 0 {
		t.Fatal("provider ownership incorrect")
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil || store.closed != 1 {
		t.Fatal("cleanup was not idempotent")
	}
	// Maintenance must not open runtime stores.
	count, err := session.CleanupConfiguredStores(t.Context(), cfg, providers, 10)
	if err != nil || count != 2 || maintenanceCloses != 1 || opens != 1 {
		t.Fatalf("count=%d closes=%d opens=%d error=%v", count, maintenanceCloses, opens, err)
	}
	if cfg.HTTP.Session.Stores["custom"]["location"] != "original" {
		t.Fatal("configuration mutated")
	}
}

func TestConfiguredProviderFailures(t *testing.T) {
	for _, mode := range []string{"unknown", "disabled", "duplicate", "nil factory", "validation", "cancellation"} {
		t.Run(mode, func(t *testing.T) {
			cfg := configuredSettings()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause := errors.New("invalid provider settings")
			store := &configuredStore{}
			providers := map[string]session.ConfiguredProvider{"custom": {
				Open: func(context.Context, map[string]string) (session.SessionStore, error) {
					if mode != "cancellation" {
						t.Fatal("invalid selection opened a store")
					}
					cancel()
					return store, nil
				},
			}}
			switch mode {
			case "unknown":
				cfg.HTTP.Session.Types["default"] = "missing"
			case "disabled":
				cfg.HTTP.Session.Stores["custom"]["enabled"] = "false"
			case "duplicate":
				providers["CUSTOM"] = providers["custom"]
			case "nil factory":
				providers["custom"] = session.ConfiguredProvider{}
			case "validation":
				p := providers["custom"]
				p.Validate = func(map[string]string) error { return cause }
				providers["custom"] = p
			}
			manager, err := session.NewConfiguredManager(ctx, cfg, providers)
			if manager != nil || err == nil {
				t.Fatal("accepted failed setup")
			}
			if mode == "cancellation" && (!errors.Is(err, context.Canceled) || store.closed != 1) {
				t.Fatal("cancellation leaked storage", err)
			}
			if mode == "validation" && !errors.Is(err, cause) {
				t.Fatal("lost provider error")
			}
		})
	}
}

func TestBuiltInConfiguredProvidersValidateBeforeOpening(t *testing.T) {
	for name, factory := range map[string]func(config.Config) session.ConfiguredProvider{
		"cookie": cookie.ConfiguredProvider,
		"sqlite": sqlite.ConfiguredProvider,
	} {
		for _, setting := range []string{"valid", "jwtKey", "tokenAge", "cookieAge"} {
			t.Run(name+"/"+setting, func(t *testing.T) {
				connection := filepath.Join(t.TempDir(), "sessions.db")
				cfg := config.Config{}
				cfg.HTTP.Session.JwtKey = strings.Repeat("s", 32)
				cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
				cfg.HTTP.Session.Stores = map[string]map[string]string{name: {
					"enabled": "true", "connection": connection, "sessiontable": "sessions",
				}}
				cfg.HTTP.Session.Types = map[string]string{"default": name}
				switch setting {
				case "jwtKey":
					cfg.HTTP.Session.JwtKey = ""
				case "tokenAge":
					cfg.HTTP.Session.TokenAge = 0
				case "cookieAge":
					cfg.HTTP.Session.CookieAge = 0
				}
				providers := map[string]session.ConfiguredProvider{name: factory(cfg)}
				if setting == "valid" {
					if err := session.ValidateConfiguredStores(&cfg, providers); err != nil {
						t.Fatal(err)
					}
				} else {
					manager, err := session.NewConfiguredManager(t.Context(), &cfg, providers)
					if manager != nil {
						manager.Close()
						t.Fatal("invalid settings opened a manager")
					}
					if err == nil || !strings.Contains(err.Error(), "http.session."+setting) {
						t.Fatalf("expected error for %s, got %v", setting, err)
					}
				}
				if _, err := os.Stat(connection); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("validation touched storage: %v", err)
				}
			})
		}
	}
}
