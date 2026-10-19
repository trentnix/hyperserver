package setup

import (
	"context"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/configured"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

// CleanupSessions cleans selected stores without initializing schema.
func CleanupSessions(ctx context.Context, cfg *config.Config, limit int) (int64, error) {
	var providers map[string]session.ConfiguredProvider
	if cfg != nil {
		providers = configured.SessionProviders(*cfg)
	}
	return session.CleanupConfiguredStores(ctx, cfg, providers, limit)
}
