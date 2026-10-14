package setup

import (
	"context"
	"fmt"
	"strings"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	"github.com/trentnix/hyperserver/pkg/services/session/sqlite"
)

type sessionProvider struct {
	open        func(context.Context, map[string]string) (session.SessionStore, error)
	maintenance func(context.Context, map[string]string) (session.ExpiredSessionMaintenance, func() error, error)
}

// sessionProviders is the reference application's built-in provider wiring.
// Framework applications supply factories directly to the session package.
func sessionProviders(cfg *config.Config) map[string]sessionProvider {
	return map[string]sessionProvider{
		"cookiestore": {
			open: func(context.Context, map[string]string) (session.SessionStore, error) {
				return cookie.NewCookieStore(cookie.Options{
					SigningKey:     cfg.HTTP.Session.JwtKey,
					TokenLifetime:  cfg.HTTP.Session.TokenAge,
					CookieLifetime: cfg.HTTP.Session.CookieAge,
				})
			},
			maintenance: func(context.Context, map[string]string) (session.ExpiredSessionMaintenance, func() error, error) {
				return nil, nil, nil
			},
		},
		"sqlitestore": {
			open: func(ctx context.Context, options map[string]string) (session.SessionStore, error) {
				connection, err := database.ResolveConnection("sqlite3", options["connection"], cfg.App.WorkingDirectory)
				if err != nil {
					return nil, err
				}
				return sqlite.NewSQLiteStore(ctx, sqlite.Options{
					Connection:     connection,
					Table:          options["sessiontable"],
					SigningKey:     cfg.HTTP.Session.JwtKey,
					TokenLifetime:  cfg.HTTP.Session.TokenAge,
					CookieLifetime: cfg.HTTP.Session.CookieAge,
				})
			},
			maintenance: func(ctx context.Context, options map[string]string) (session.ExpiredSessionMaintenance, func() error, error) {
				connection, err := database.ResolveConnection("sqlite3", options["connection"], cfg.App.WorkingDirectory)
				if err != nil {
					return nil, nil, err
				}
				return sqlite.OpenMaintenance(ctx, connection, options["sessiontable"])
			},
		},
	}
}

// NewSessionManager initializes only the reference application's selected stores.
// The caller must close the manager after its consumers stop.
func NewSessionManager(ctx context.Context, cfg *config.Config) (*session.SessionManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("session configuration is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateSessionConfig(cfg); err != nil {
		return nil, err
	}
	factories := make(map[string]session.StoreFactory)
	for name, provider := range sessionProviders(cfg) {
		factories[name] = func(ctx context.Context) (session.SessionStore, error) {
			options, err := selectedSessionOptions(cfg, name)
			if err != nil {
				return nil, err
			}
			return provider.open(ctx, options)
		}
	}
	return session.NewSessionManager(ctx, cfg.HTTP.Session.Types, factories)
}

// CleanupSessions opens maintenance storage for selected providers without
// creating tables or requiring signing keys. Each provider owns its storage setup.
func CleanupSessions(ctx context.Context, cfg *config.Config, limit int) (int64, error) {
	if cfg == nil {
		return 0, fmt.Errorf("session configuration is required")
	}
	factories := make(map[string]session.MaintenanceFactory)
	for name, provider := range sessionProviders(cfg) {
		factories[name] = func(ctx context.Context) (session.ExpiredSessionMaintenance, func() error, error) {
			options, err := selectedSessionOptions(cfg, name)
			if err != nil {
				return nil, nil, err
			}
			return provider.maintenance(ctx, options)
		}
	}
	return session.CleanupExpired(ctx, cfg.HTTP.Session.Types, factories, limit)
}

func selectedSessionOptions(cfg *config.Config, name string) (map[string]string, error) {
	var options map[string]string
	found := false
	for key, values := range cfg.HTTP.Session.Stores {
		if strings.EqualFold(key, name) {
			if found {
				return nil, fmt.Errorf("duplicate session store %q", name)
			}
			options, found = values, true
		}
	}
	if !found {
		return nil, fmt.Errorf("selected session store %q is not configured", name)
	}
	enabled, err := config.ProviderEnabled("http.session.stores."+name+".enabled", options["enabled"])
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, session.NewErrStoreDisabled(nil)
	}
	return options, nil
}
