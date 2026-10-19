package configured_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/configured"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type tokenMaintenanceFunc func(context.Context, int) (int64, error)

func (f tokenMaintenanceFunc) DeleteExpiredTokens(ctx context.Context, limit int) (int64, error) {
	return f(ctx, limit)
}

func TestAccountMaintenanceSelectsOnlyConfiguredProvider(t *testing.T) {
	for _, name := range []string{"", "sqlite", "custom", "unknown", "disabled"} {
		t.Run("provider="+name, func(t *testing.T) {
			cfg := &config.Config{Auth: config.AuthConfig{
				Enabled:        name != "disabled",
				AccountStorage: config.AccountStorageConfig{Provider: name, Options: map[string]string{"location": "original"}},
			}}
			opens, closes := 0, 0
			provider := user.StorageProvider{
				Open: func(context.Context, map[string]string) (user.AccountRepository, func() error, error) {
					t.Fatal("maintenance opened runtime storage")
					return nil, nil, nil
				},
				OpenMaintenance: func(ctx context.Context, options map[string]string) (user.TokenMaintenance, func() error, error) {
					opens++
					if options["location"] != "original" {
						t.Fatal("lost provider options")
					}
					options["location"] = "changed"
					return tokenMaintenanceFunc(func(got context.Context, limit int) (int64, error) {
						if got != ctx || got != t.Context() || limit != 3 {
							t.Fatal("lost context or batch limit")
						}
						return 2, nil
					}), func() error { closes++; return nil }, nil
				},
			}
			providers := map[string]user.StorageProvider{"sqlite": provider, "custom": provider, "disabled": provider}
			count, err := configured.CleanupAccountTokens(t.Context(), cfg, 3, providers)
			wantCalls := 1
			if name == "unknown" || name == "disabled" {
				wantCalls = 0
			}
			if (err != nil) != (name == "unknown") || count != int64(2*wantCalls) || opens != wantCalls || closes != wantCalls {
				t.Fatalf("count=%d opens=%d closes=%d error=%v", count, opens, closes, err)
			}
			if cfg.Auth.AccountStorage.Provider != name || cfg.Auth.AccountStorage.Options["location"] != "original" || len(providers) != 3 {
				t.Fatal("maintenance changed caller configuration or catalog")
			}
		})
	}
}

func TestAccountMaintenanceFailureClosesPartialResources(t *testing.T) {
	cause, closeErr := errors.New("open failed"), errors.New("close failed")
	closes := 0
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true}}
	count, err := configured.CleanupAccountTokens(t.Context(), cfg, 1, map[string]user.StorageProvider{"sqlite": {
		OpenMaintenance: func(context.Context, map[string]string) (user.TokenMaintenance, func() error, error) {
			return nil, func() error { closes++; return closeErr }, cause
		},
	}})
	if count != 0 || closes != 1 || !errors.Is(err, cause) || !errors.Is(err, closeErr) {
		t.Fatalf("failed maintenance: count=%d closes=%d error=%v", count, closes, err)
	}
	if _, err := configured.CleanupAccountTokens(t.Context(), nil, 1, nil); err == nil {
		t.Fatal("nil configuration accepted")
	}
}

func TestAccountProviderCatalogIsIndependent(t *testing.T) {
	directory := t.TempDir()
	cfg := config.Config{
		App:      config.AppConfig{WorkingDirectory: directory},
		Database: config.DatabaseConfig{Driver: "sqlite3", Connection: "accounts.db"},
	}
	first, second := configured.AccountProviders(cfg), configured.AccountProviders(cfg)
	delete(first, "sqlite")
	provider := second["sqlite"]
	if provider.Open == nil || provider.OpenMaintenance == nil {
		t.Fatal("catalog entries were shared or a factory is missing")
	}
	if _, err := os.Stat(filepath.Join(directory, "accounts.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("catalog construction touched storage: %v", err)
	}
	cfg.Database.Connection = "must-not-open.db"
	repository, closeRepository, err := provider.Open(t.Context(), nil)
	if closeRepository != nil {
		t.Cleanup(func() { closeRepository() })
	}
	if err != nil || repository == nil || closeRepository == nil {
		t.Fatalf("runtime factory: repository=%v error=%v", repository, err)
	}
	if err := closeRepository(); err != nil {
		t.Fatal(err)
	}

	maintenance, closeMaintenance, err := provider.OpenMaintenance(t.Context(), nil)
	if closeMaintenance != nil {
		t.Cleanup(func() { closeMaintenance() })
	}
	if err != nil || maintenance == nil || closeMaintenance == nil {
		t.Fatalf("maintenance factory: maintenance=%v error=%v", maintenance, err)
	}
	if count, err := maintenance.DeleteExpiredTokens(t.Context(), 1); err != nil || count != 0 {
		t.Fatalf("maintenance did not use the runtime schema: count=%d error=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "must-not-open.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("factories used mutated configuration: %v", err)
	}
}
