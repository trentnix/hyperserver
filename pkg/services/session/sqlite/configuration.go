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
// Options accept enabled, connection, and sessionTable. The spelling sessiontable
// is also accepted, but the two table options must not appear together.
// Relative connections use the application's resolved resource directory.
// Runtime setup owns its pool. Maintenance opens existing storage only.
func ConfiguredProvider(cfg config.Config) session.ConfiguredProvider {
	directory, settings := cfg.App.WorkingDirectory, cfg.HTTP.Session
	return session.ConfiguredProvider{
		Validate: func(options map[string]string) error {
			if _, err := configuredTable(options); err != nil {
				return err
			}
			return sessioncookie.ValidateConfig(settings.JwtKey, settings.TokenAge, settings.CookieAge)
		},
		Open: func(ctx context.Context, options map[string]string) (session.SessionStore, error) {
			table, err := configuredTable(options)
			if err != nil {
				return nil, err
			}
			connection, err := database.ResolveConnection("sqlite3", options["connection"], directory)
			if err != nil {
				return nil, err
			}
			return NewSQLiteStore(ctx, Options{
				Connection:     connection,
				Table:          table,
				SigningKey:     settings.JwtKey,
				TokenLifetime:  settings.TokenAge,
				CookieLifetime: settings.CookieAge,
			})
		},
		OpenMaintenance: func(ctx context.Context, options map[string]string) (session.ExpiredSessionMaintenance, func() error, error) {
			table, err := configuredTable(options)
			if err != nil {
				return nil, nil, err
			}
			connection, err := database.ResolveConnection("sqlite3", options["connection"], directory)
			if err != nil {
				return nil, nil, err
			}
			return OpenMaintenance(ctx, connection, table)
		},
	}
}

func configuredTable(options map[string]string) (string, error) {
	for name := range options {
		switch name {
		case "enabled", "connection", "sessionTable", "sessiontable":
		default:
			return "", errors.New("SQLite sessions accept only enabled, connection, and sessionTable options")
		}
	}
	table, present := options["sessionTable"]
	if legacy, ok := options["sessiontable"]; ok {
		if present {
			return "", errors.New("specify only one of sessionTable and sessiontable")
		}
		table = legacy
	}
	if strings.TrimSpace(options["connection"]) == "" || strings.TrimSpace(table) == "" {
		return "", errors.New("requires connection and sessionTable")
	}
	return table, nil
}
