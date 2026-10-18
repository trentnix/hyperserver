package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// OwnedProvider opens its own pool using cfg.Database. Unlike ConfiguredProvider,
// it needs no application-supplied pool. The returned cleanup owns that pool,
// including when initialization fails. Use through user.OpenConfiguredStorage.
// Construction performs no I/O. Maintenance opens existing storage only.
func OwnedProvider(cfg config.Config) user.StorageProvider {
	cfg = cfg.Clone()
	provider := ConfiguredProvider(cfg, nil)
	provider.Open = func(ctx context.Context, options map[string]string) (user.AccountRepository, func() error, error) {
		if err := ValidateOptions(options); err != nil {
			return nil, nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		pool, err := dbsqlite.OpenConfigured(cfg.Database, cfg.App.WorkingDirectory)
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
// an existing connection from cfg.Database, without creating or migrating schema.
// Options are checked by ValidateOptions. Construction performs no I/O.
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
			if err := ValidateOptions(options); err != nil {
				return nil, nil, err
			}
			if cfg.Database.Driver != "sqlite3" {
				return nil, nil, errors.New("SQLite account maintenance requires the sqlite3 database driver")
			}
			connection, err := database.ResolveConnection(cfg.Database.Driver, cfg.Database.Connection, cfg.App.WorkingDirectory)
			if err != nil {
				return nil, nil, err
			}
			return OpenTokenMaintenance(ctx, connection)
		},
	}
}
