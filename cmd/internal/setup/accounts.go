// Package setup selects storage providers for the reference maintenance command.
package setup

import (
	"context"
	"errors"
	"maps"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

// CleanupAccountTokens removes one bounded batch without initializing account
// schema. Providers close acquired resources. The caller owns the deadline.
func CleanupAccountTokens(ctx context.Context, cfg *config.Config, limit int) (int64, error) {
	return cleanupAccountTokens(ctx, cfg, limit, nil)
}

func cleanupAccountTokens(ctx context.Context, cfg *config.Config, limit int, additional map[string]user.StorageProvider) (int64, error) {
	if limit < 1 {
		return 0, errors.New("token cleanup limit must be positive")
	}
	if cfg == nil {
		return 0, errors.New("account configuration is required")
	}
	if !cfg.Auth.Enabled {
		return 0, nil
	}

	selection := cfg.Auth.AccountStorage
	if selection.Provider == "" {
		selection.Provider = "sqlite"
	}
	providers := map[string]user.StorageProvider{"sqlite": usersqlite.ConfiguredProvider(*cfg, nil)}
	maps.Copy(providers, additional)
	return user.CleanupConfiguredTokens(ctx, selection, providers, limit)
}
