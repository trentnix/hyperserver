// Package reference selects service providers for the development web and cleanup commands.
// Framework applications choose and initialize their own providers.
package reference

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/user"
	usersqlite "github.com/trentnix/hyperserver/pkg/services/user/sqlite"
)

const accountSetupTimeout = 10 * time.Second

// AccountStorage selects account storage using the reference application's configuration.
// It does not own repositories or register shutdown hooks. Use serially during
// startup or maintenance, never during requests.
type AccountStorage struct {
	config    *config.Config
	providers map[string]AccountProvider
}

// NewAccountStorage selects account storage from the loaded configuration.
// Construction does not open connections or initialize storage.
func NewAccountStorage(cfg *config.Config) *AccountStorage {
	return &AccountStorage{config: cfg}
}

// AccountProvider supplies account storage and optional token maintenance.
// Factories must honor ctx and validate their private copy of options without
// exposing secrets in errors. Each optional close function releases only owned
// resources, never borrowed pools. Open calls close on failure even when the
// factory returns an error. After a successful Open, the caller owns cleanup.
// CleanupTokens closes maintenance storage after each batch.
type AccountProvider struct {
	// Open creates a ready-to-use repository. It may initialize storage.
	Open func(ctx context.Context, options map[string]string) (user.AccountRepository, func() error, error)
	// OpenMaintenance opens existing storage without creating or migrating tables.
	// Nil means that this provider does not support explicit token maintenance.
	OpenMaintenance func(ctx context.Context, options map[string]string) (user.TokenMaintenance, func() error, error)
}

// RegisterProvider adds a provider to this application's account setup. Names are
// case-sensitive. The built-in name "sqlite" is reserved. Register serially
// before initialization or maintenance. Duplicate names and nil Open factories are rejected.
func (s *AccountStorage) RegisterProvider(name string, provider AccountProvider) error {
	if name == "" || strings.TrimSpace(name) != name {
		return errors.New("account provider name must not be empty or have surrounding whitespace")
	}
	if provider.Open == nil {
		return fmt.Errorf("account provider %q has no factory", name)
	}
	if _, exists := s.providers[name]; name == "sqlite" || exists {
		return fmt.Errorf("account provider %q is already registered", name)
	}

	if s.providers == nil {
		s.providers = make(map[string]AccountProvider)
	}
	s.providers[name] = provider
	return nil
}

// Open prepares the selected provider when authentication is enabled.
// SQLite borrows pool. Other providers open their own storage.
// The caller owns the returned cleanup function. Failed opens release acquired
// resources and can be retried. Disabled authentication returns nil values.
func (s *AccountStorage) Open(ctx context.Context, pool *sqlx.DB) (user.AccountRepository, func() error, error) {
	if !s.config.Auth.Enabled {
		return nil, nil, nil
	}

	name, provider, err := accountProvider(s.config, pool, s.providers)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, accountSetupTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	repository, cleanup, err := provider.Open(ctx, maps.Clone(s.config.Auth.AccountStorage.Options))
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && repository == nil {
		err = errors.New("provider returned no account repository")
	}
	if err != nil {
		if cleanup != nil {
			err = errors.Join(err, cleanup())
		}
		return nil, nil, fmt.Errorf("initialize account provider %q: %w", name, err)
	}
	return repository, cleanup, nil
}

func initializeSQLiteAccounts(ctx context.Context, pool *sqlx.DB, options map[string]string) (user.AccountRepository, func() error, error) {
	if err := usersqlite.ValidateOptions(options); err != nil {
		return nil, nil, err
	}

	repository := usersqlite.NewAccountRepository(pool)
	if err := repository.Initialize(ctx); err != nil {
		return nil, nil, err
	}
	return repository, nil, nil
}

func accountProvider(cfg *config.Config, pool *sqlx.DB, providers map[string]AccountProvider) (string, AccountProvider, error) {
	name := cfg.Auth.AccountStorage.Provider
	if name == "" {
		name = "sqlite"
	}

	if name == "sqlite" {
		return name, AccountProvider{
			Open: func(ctx context.Context, options map[string]string) (user.AccountRepository, func() error, error) {
				return initializeSQLiteAccounts(ctx, pool, options)
			},
			OpenMaintenance: func(ctx context.Context, options map[string]string) (user.TokenMaintenance, func() error, error) {
				databaseConfig := cfg.Database
				var err error
				databaseConfig.Connection, err = database.ResolveConnection(databaseConfig.Driver, databaseConfig.Connection, cfg.App.WorkingDirectory)
				if err != nil {
					return nil, nil, err
				}
				return usersqlite.OpenTokenMaintenance(ctx, databaseConfig, options)
			},
		}, nil
	}

	provider, ok := providers[name]
	if !ok {
		return name, AccountProvider{}, fmt.Errorf("auth.accountStorage.provider: unknown provider %q", name)
	}
	return name, provider, nil
}

// CleanupAccountTokens removes one bounded batch using the reference application's
// built-in account provider without initializing account schema. It closes acquired
// resources after the batch. The caller owns cancellation and the deadline.
func CleanupAccountTokens(ctx context.Context, cfg *config.Config, limit int) (int64, error) {
	return cleanupAccountTokens(ctx, cfg, limit, nil)
}

// CleanupTokens runs the same maintenance operation with this instance's registered
// providers available. It does not open a runtime repository or initialize storage.
func (s *AccountStorage) CleanupTokens(ctx context.Context, limit int) (int64, error) {
	return cleanupAccountTokens(ctx, s.config, limit, s.providers)
}

func cleanupAccountTokens(ctx context.Context, cfg *config.Config, limit int, providers map[string]AccountProvider) (count int64, err error) {
	if limit < 1 {
		return 0, errors.New("token cleanup limit must be positive")
	}
	if !cfg.Auth.Enabled {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	name, provider, err := accountProvider(cfg, nil, providers)
	if err != nil {
		return 0, err
	}
	if provider.OpenMaintenance == nil {
		return 0, fmt.Errorf("account provider %q does not support token maintenance", name)
	}

	maintenance, closeStore, err := provider.OpenMaintenance(ctx, maps.Clone(cfg.Auth.AccountStorage.Options))
	if closeStore != nil {
		defer func() { err = errors.Join(err, closeStore()) }()
	}
	if err != nil {
		return 0, fmt.Errorf("open account maintenance %q: %w", name, err)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if maintenance == nil {
		return 0, fmt.Errorf("account provider %q returned no token maintenance", name)
	}

	return maintenance.DeleteExpiredTokens(ctx, limit)
}
