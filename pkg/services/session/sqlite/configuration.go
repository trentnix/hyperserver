package sqlite

import (
	"context"
	"errors"
	"strings"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/internal/sessioncookie"
)

// ConfiguredProvider adapts SQLite sessions to configuration-driven setup.
// Options accept enabled, connection, and sessiontable, as normalized by config.GetConfig.
// Relative connections use the application's resolved resource directory.
// Runtime setup owns its pool. Maintenance opens existing storage only.
func ConfiguredProvider(cfg config.Config) session.ConfiguredProvider {
	directory, settings := cfg.App.WorkingDirectory, cfg.HTTP.Session
	return session.ConfiguredProvider{
		Validate: func(options map[string]string) error {
			if err := validateOptions(options); err != nil {
				return err
			}
			return sessioncookie.ValidateConfig(settings.JwtKey, settings.TokenAge, settings.CookieAge)
		},
		Open: func(ctx context.Context, options map[string]string) (session.SessionStore, error) {
			if err := validateOptions(options); err != nil {
				return nil, err
			}
			connection, err := database.ResolveConnection("sqlite3", options["connection"], directory)
			if err != nil {
				return nil, err
			}
			return NewSQLiteStore(ctx, Options{
				Connection:     connection,
				Table:          options["sessiontable"],
				SigningKey:     settings.JwtKey,
				TokenLifetime:  settings.TokenAge,
				CookieLifetime: settings.CookieAge,
			})
		},
		OpenMaintenance: func(ctx context.Context, options map[string]string) (session.ExpiredSessionMaintenance, func() error, error) {
			if err := validateOptions(options); err != nil {
				return nil, nil, err
			}
			connection, err := database.ResolveConnection("sqlite3", options["connection"], directory)
			if err != nil {
				return nil, nil, err
			}
			return OpenMaintenance(ctx, connection, options["sessiontable"])
		},
	}
}

func validateOptions(options map[string]string) error {
	for name := range options {
		switch name {
		case "enabled", "connection", "sessiontable":
		default:
			return errors.New("SQLite sessions accept only enabled, connection, and sessiontable options")
		}
	}
	if strings.TrimSpace(options["connection"]) == "" || strings.TrimSpace(options["sessiontable"]) == "" {
		return errors.New("requires connection and sessionTable")
	}
	return nil
}
