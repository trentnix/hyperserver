// Package configured supplies built-in account and session providers for
// configuration-driven setup. Importing it opens no resources. Applications
// can add or replace factories without changing the service contracts.
package configured

import (
	"context"
	"errors"
	"maps"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

// OpenAccounts opens auth.accountStorage when authentication is enabled.
// SQLite is available as "sqlite", the default for an empty provider name.
// Additional factories replace matching names.
// Only the selected factory runs. If cleanup is non-nil, the caller must call it
// after consumers stop. Failed opens close partial resources. Disabled
// authentication opens nothing. Configuration paths must already be resolved.
func OpenAccounts(ctx context.Context, cfg *config.Config, additional map[string]user.StorageProvider) (user.AccountRepository, func() error, error) {
	if cfg == nil {
		return nil, nil, errors.New("account configuration is required")
	}
	if !cfg.Auth.Enabled {
		return nil, nil, nil
	}
	selection, providers := accountStorage(*cfg, additional)
	return user.OpenConfiguredStorage(ctx, selection, providers)
}

// CleanupAccountTokens removes one bounded batch using the same provider selection
// as OpenAccounts. It opens existing storage without initializing schema and closes
// acquired resources. Disabled authentication opens nothing. Configuration paths
// must already be resolved. The caller controls cancellation and deadlines.
func CleanupAccountTokens(ctx context.Context, cfg *config.Config, limit int, additional map[string]user.StorageProvider) (int64, error) {
	if limit < 1 {
		return 0, errors.New("token cleanup limit must be positive")
	}
	if cfg == nil {
		return 0, errors.New("account configuration is required")
	}
	if !cfg.Auth.Enabled {
		return 0, nil
	}
	selection, providers := accountStorage(*cfg, additional)
	return user.CleanupConfiguredTokens(ctx, selection, providers, limit)
}

// AccountProviders returns fresh built-in factories for runtime setup or maintenance.
// Construction performs no I/O. Callers can add or replace entries.
func AccountProviders(cfg config.Config) map[string]user.StorageProvider {
	return map[string]user.StorageProvider{"sqlite": usersqlite.OwnedProvider(cfg)}
}

func accountStorage(cfg config.Config, additional map[string]user.StorageProvider) (config.AccountStorageConfig, map[string]user.StorageProvider) {
	selection := cfg.Auth.AccountStorage
	if selection.Provider == "" {
		selection.Provider = "sqlite"
	}
	providers := AccountProviders(cfg)
	maps.Copy(providers, additional)
	return selection, providers
}
