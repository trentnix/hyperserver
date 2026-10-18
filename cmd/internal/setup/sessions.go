package setup

import (
	"context"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	"github.com/trentnix/hyperserver/pkg/services/session/sqlite"
)

// sessionProviders lists adapters available to the reference commands.
func sessionProviders(cfg *config.Config) map[string]session.ConfiguredProvider {
	if cfg == nil {
		return nil
	}
	return map[string]session.ConfiguredProvider{
		"cookiestore": cookie.ConfiguredProvider(*cfg),
		"sqlitestore": sqlite.ConfiguredProvider(*cfg),
	}
}

// NewSessionManager opens the reference application's selected session stores.
func NewSessionManager(ctx context.Context, cfg *config.Config) (*session.SessionManager, error) {
	return session.NewConfiguredManager(ctx, cfg, sessionProviders(cfg))
}

// ValidateSessionConfig checks the reference application's selected stores without I/O.
func ValidateSessionConfig(cfg *config.Config) error {
	return session.ValidateConfiguredStores(cfg, sessionProviders(cfg))
}

// CleanupSessions cleans selected stores without initializing schema.
func CleanupSessions(ctx context.Context, cfg *config.Config, limit int) (int64, error) {
	return session.CleanupConfiguredStores(ctx, cfg, sessionProviders(cfg), limit)
}
