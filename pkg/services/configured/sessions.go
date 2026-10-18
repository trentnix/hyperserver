package configured

import (
	"context"
	"errors"
	"maps"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	sessionsqlite "github.com/trentnix/hyperserver/pkg/services/session/sqlite"
)

// NewSessionManager opens the stores selected by http.session configuration.
// Built-in names are "cookiestore" and "sqlitestore". Additional factories
// replace matching names, using lowercase keys. The manager owns opened stores
// and must be closed after consumers stop. Failed opens close partial resources.
// Configuration paths must already be resolved.
func NewSessionManager(ctx context.Context, cfg *config.Config, additional map[string]session.ConfiguredProvider) (*session.SessionManager, error) {
	if cfg == nil {
		return nil, errors.New("session configuration is required")
	}
	settings := cfg.Clone()
	providers := SessionProviders(settings)
	maps.Copy(providers, additional)
	return session.NewConfiguredManager(ctx, &settings, providers)
}

// SessionProviders returns fresh built-in factories for runtime setup, validation,
// or maintenance. Construction performs no I/O. Callers can add or replace entries.
func SessionProviders(cfg config.Config) map[string]session.ConfiguredProvider {
	cfg = cfg.Clone()
	return map[string]session.ConfiguredProvider{
		"cookiestore": cookie.ConfiguredProvider(cfg),
		"sqlitestore": sessionsqlite.ConfiguredProvider(cfg),
	}
}
