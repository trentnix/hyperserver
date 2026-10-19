package session_test

import (
	"context"
	"errors"
	"maps"
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

func (s *configuredStore) Close() error { s.closed++; return nil }

type configuredMaintenance struct{}

func TestFileAndProgrammaticSessionNamesSelectSameStores(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.yaml", []byte("http:\n  session:\n    types:\n      default: fallback\n      ShoppingCart: chosen\n      shoppingcart: other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "HYPERSERVER_") {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, types := range []map[string]string{cfg.HTTP.Session.Types, {"default": "fallback", "ShoppingCart": "chosen", "shoppingcart": "other"}} {
		stores := map[string]*configuredStore{"fallback": {}, "chosen": {}, "other": {}}
		factories := make(map[string]session.StoreFactory)
		for name, store := range stores {
			factories[name] = func(context.Context) (session.SessionStore, error) { return store, nil }
		}
		manager, err := session.NewSessionManager(t.Context(), types, factories)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = manager.Close() })
		for name, provider := range map[string]string{"ShoppingCart": "chosen", "shoppingcart": "other", "unmapped": "fallback"} {
			got, err := manager.Store(name)
			if err != nil || got != stores[provider] {
				t.Fatalf("Store(%q) = %v, %v. Want %s", name, got, err, provider)
			}
		}
	}
}

func (*configuredMaintenance) DeleteExpired(context.Context, int) (int64, error) { return 2, nil }

func configuredSettings() *config.Config {
	cfg := &config.Config{}
	// Custom providers need not use the built-in JWT and cookie settings.
	cfg.HTTP.Session.Stores = map[string]map[string]string{"custom": {"enabled": "true", "location": "original"}}
	cfg.HTTP.Session.Types = map[string]string{"default": "CUSTOM", "another": "custom"}
	return cfg
}

func TestMaintenanceOnlyConfiguredProvider(t *testing.T) {
	opens, closes := 0, 0
	provider := session.ConfiguredProvider{
		Validate: func(map[string]string) error {
			t.Fatal("maintenance ran runtime validation")
			return nil
		},
		OpenMaintenance: func(ctx context.Context, options map[string]string) (session.ExpiredSessionMaintenance, func() error, error) {
			opens++
			if ctx != t.Context() || options["location"] != "original" {
				t.Fatal("lost context or options")
			}
			return &configuredMaintenance{}, func() error { closes++; return nil }, nil
		},
	}
	providers := map[string]session.ConfiguredProvider{"Custom": provider, "unused": {}}
	cfg := configuredSettings()
	count, err := session.CleanupConfiguredStores(t.Context(), cfg, providers, 10)
	if err != nil || count != 2 || opens != 1 || closes != 1 {
		t.Fatalf("count=%d opens=%d closes=%d error=%v", count, opens, closes, err)
	}
	if _, err := session.NewConfiguredManager(t.Context(), cfg, providers); err == nil || !strings.Contains(err.Error(), "no runtime factory") {
		t.Fatalf("runtime accepted maintenance-only provider: %v", err)
	}
	provider.OpenMaintenance = nil
	providers["Custom"] = provider
	if _, err := session.CleanupConfiguredStores(t.Context(), cfg, providers, 10); err == nil || !strings.Contains(err.Error(), "does not support maintenance") {
		t.Fatalf("maintenance accepted missing factory: %v", err)
	}
	for _, name := range []string{"CUSTOM", " bad ", ""} {
		invalid := map[string]session.ConfiguredProvider{"Custom": provider, name: provider}
		if _, err := session.CleanupConfiguredStores(t.Context(), cfg, invalid, 10); err == nil {
			t.Fatalf("accepted duplicate or invalid provider %q", name)
		}
	}
}

func TestSQLiteConfiguredTableOptionSpellings(t *testing.T) {
	for _, spelling := range []string{"sessionTable", "sessiontable"} {
		t.Run(spelling, func(t *testing.T) {
			cfg := validSessionConfig()
			provider := sqlite.ConfiguredProvider(*cfg)
			options := map[string]string{"enabled": "true", "connection": filepath.Join(t.TempDir(), "sessions.db"), spelling: "custom_sessions"}
			if err := provider.Validate(options); err != nil {
				t.Fatal(err)
			}
			store, err := provider.Open(t.Context(), options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.(*sqlite.SQLiteStore).Close() })
			maintenance, closeStore, err := provider.OpenMaintenance(t.Context(), options)
			if closeStore != nil {
				t.Cleanup(func() { _ = closeStore() })
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := maintenance.DeleteExpired(t.Context(), 1); err != nil {
				t.Fatal(err)
			}
		})
	}
	provider := sqlite.ConfiguredProvider(*validSessionConfig())
	options := map[string]string{"connection": "unused.db", "sessionTable": "one", "sessiontable": "two"}
	if err := provider.Validate(options); err == nil {
		t.Fatal("validation accepted conflicting table options")
	}
	if _, err := provider.Open(t.Context(), options); err == nil {
		t.Fatal("runtime accepted conflicting table options")
	}
	if _, _, err := provider.OpenMaintenance(t.Context(), options); err == nil {
		t.Fatal("maintenance accepted conflicting table options")
	}
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
				cfg.HTTP.Session.Stores = map[string]map[string]string{name: {"enabled": "true"}}
				if name == "sqlite" {
					cfg.HTTP.Session.Stores[name]["connection"] = connection
					cfg.HTTP.Session.Stores[name]["sessiontable"] = "sessions"
				}
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

func TestBuiltInProvidersRejectUnknownOptions(t *testing.T) {
	for name, factory := range map[string]func(config.Config) session.ConfiguredProvider{
		"cookie": cookie.ConfiguredProvider,
		"sqlite": sqlite.ConfiguredProvider,
	} {
		for _, option := range []string{"typo", "cookieage", "enabled "} {
			t.Run(name+"/"+option, func(t *testing.T) {
				cfg := config.Config{App: config.AppConfig{WorkingDirectory: t.TempDir()}}
				cfg.HTTP.Session.JwtKey = strings.Repeat("s", 32)
				cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
				options := map[string]string{"enabled": "true", option: "secret-option-value"}
				if name == "sqlite" {
					options["connection"], options["sessiontable"] = "sessions.db", "sessions"
				}
				original := maps.Clone(options)
				cfg.HTTP.Session.Stores = map[string]map[string]string{name: options}
				cfg.HTTP.Session.Types = map[string]string{"default": name}
				provider := factory(cfg)
				providers := map[string]session.ConfiguredProvider{name: provider}
				checkError := func(err error) {
					t.Helper()
					if err == nil || !strings.Contains(err.Error(), "accept only") || strings.Contains(err.Error(), "secret-option-value") {
						t.Fatalf("expected safe option error, got %v", err)
					}
				}

				checkError(provider.Validate(options))
				store, err := provider.Open(t.Context(), options)
				checkError(err)
				if store != nil {
					t.Fatal("invalid options returned a store")
				}
				maintenance, closeStore, err := provider.OpenMaintenance(t.Context(), options)
				checkError(err)
				if maintenance != nil || closeStore != nil {
					t.Fatal("invalid options acquired maintenance resources")
				}
				manager, err := session.NewConfiguredManager(t.Context(), &cfg, providers)
				checkError(err)
				if manager != nil || !strings.Contains(err.Error(), "http.session.stores."+name) {
					t.Fatalf("invalid manager or missing configuration path: %v", err)
				}
				count, err := session.CleanupConfiguredStores(t.Context(), &cfg, providers, 1)
				checkError(err)
				if count != 0 || !maps.Equal(options, original) {
					t.Fatal("invalid options changed storage or configuration")
				}
				entries, err := os.ReadDir(cfg.App.WorkingDirectory)
				if err != nil || len(entries) != 0 {
					t.Fatalf("invalid options touched storage: %v %v", entries, err)
				}
			})
		}
	}
}

func TestBuiltInMaintenanceDoesNotRequireCookieSettings(t *testing.T) {
	for name, factory := range map[string]func(config.Config) session.ConfiguredProvider{
		"cookie": cookie.ConfiguredProvider,
		"sqlite": sqlite.ConfiguredProvider,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Config{App: config.AppConfig{WorkingDirectory: t.TempDir()}}
			cfg.HTTP.Session.JwtKey = strings.Repeat("s", 32)
			cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
			cfg.HTTP.Session.Types = map[string]string{"default": name}
			cfg.HTTP.Session.Stores = map[string]map[string]string{name: {"enabled": "true"}}
			if name == "sqlite" {
				cfg.HTTP.Session.Stores[name]["connection"] = "sessions.db"
				cfg.HTTP.Session.Stores[name]["sessiontable"] = "sessions"
			}
			manager, err := session.NewConfiguredManager(t.Context(), &cfg, map[string]session.ConfiguredProvider{name: factory(cfg)})
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}

			cfg.HTTP.Session.JwtKey = ""
			cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = 0, 0
			count, err := session.CleanupConfiguredStores(t.Context(), &cfg, map[string]session.ConfiguredProvider{name: factory(cfg)}, 1)
			if err != nil || count != 0 {
				t.Fatalf("maintenance required runtime settings: count=%d err=%v", count, err)
			}
		})
	}
}
