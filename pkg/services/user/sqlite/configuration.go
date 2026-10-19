package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// OwnedProvider opens its own pool using the required connection option.
// Unlike ConfiguredProvider, it needs no application-supplied pool. Cleanup owns that pool,
// including when initialization fails. Use through user.OpenConfiguredStorage.
// Construction performs no I/O. Maintenance opens existing storage only.
func OwnedProvider(cfg config.Config) user.StorageProvider {
	cfg = cfg.Clone()
	provider := ConfiguredProvider(cfg, nil)
	provider.Open = func(ctx context.Context, options map[string]string) (user.AccountRepository, func() error, error) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		connection, err := configuredConnection(options, cfg.App.WorkingDirectory)
		if err != nil {
			return nil, nil, err
		}
		pool, err := dbsqlite.Open(connection)
		if err != nil {
			return nil, nil, err
		}
		repository := NewAccountRepository(pool)
		return repository, pool.Close, repository.Initialize(ctx)
	}
	return provider
}

// ConfiguredProvider adapts SQLite accounts to configuration-driven setup.
// Runtime storage borrows pool and never closes it. Maintenance opens and owns
// an existing connection from the connection option, without creating or migrating
// schema. That option must name the pool's database when using maintenance.
// Runtime setup permits omitted connection options for an application-supplied pool.
// Relative paths use cfg.App.WorkingDirectory. Construction performs no I/O.
func ConfiguredProvider(cfg config.Config, pool *sql.DB) user.StorageProvider {
	return user.StorageProvider{
		Open: func(ctx context.Context, options map[string]string) (user.AccountRepository, func() error, error) {
			if err := ValidateOptions(options); err != nil {
				return nil, nil, err
			}
			repository := NewAccountRepository(pool)
			if err := repository.Initialize(ctx); err != nil {
				return nil, nil, err
			}
			return repository, nil, nil
		},
		OpenMaintenance: func(ctx context.Context, options map[string]string) (user.TokenMaintenance, func() error, error) {
			connection, err := configuredConnection(options, cfg.App.WorkingDirectory)
			if err != nil {
				return nil, nil, err
			}
			return OpenTokenMaintenance(ctx, connection)
		},
	}
}

// ValidateOptions accepts only connection. A supplied connection must not be blank.
// Opening owned storage or maintenance also requires that option to be present.
func ValidateOptions(options map[string]string) error {
	for name := range options {
		if name != "connection" {
			return errors.New("auth.accountStorage.options: sqlite accepts only connection")
		}
	}
	if connection, exists := options["connection"]; exists && strings.TrimSpace(connection) == "" {
		return errors.New("auth.accountStorage.options.connection is required")
	}
	return nil
}

func configuredConnection(options map[string]string, directory string) (string, error) {
	if err := ValidateOptions(options); err != nil {
		return "", err
	}
	if options["connection"] == "" {
		return "", errors.New("auth.accountStorage.options.connection is required")
	}
	connection, err := database.ResolveConnection("sqlite3", options["connection"], directory)
	if err != nil {
		return "", errors.New("auth.accountStorage.options.connection: invalid SQLite connection path or options")
	}
	return connection, nil
}
