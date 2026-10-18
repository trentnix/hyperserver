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
	settings := cfg.Clone()
	if settings.Auth.AccountStorage.Provider == "" {
		settings.Auth.AccountStorage.Provider = "sqlite"
	}
	providers := map[string]user.StorageProvider{"sqlite": usersqlite.OwnedProvider(settings)}
	maps.Copy(providers, additional)
	return user.OpenConfiguredStorage(ctx, settings.Auth.AccountStorage, providers)
}
