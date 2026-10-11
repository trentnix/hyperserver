// Package stores supplies the built-in cookie and SQLite session providers.
// Applications opt into these implementations through explicit setup.
package stores

import (
	"context"
	"fmt"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

// NewSessionManager validates built-in provider settings and initializes only
// selected stores. The caller must close the manager after its consumers stop.
func NewSessionManager(ctx context.Context, cfg *config.Config) (*session.SessionManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("session configuration is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}

	return session.NewSessionManager(ctx, cfg.HTTP.Session.Types, map[string]session.StoreFactory{
		cookieStoreName: func(context.Context) (session.SessionStore, error) { return NewCookieStore(cfg) },
		sqliteStoreName: func(ctx context.Context) (session.SessionStore, error) { return NewSQLiteStore(ctx, cfg) },
	})
}
