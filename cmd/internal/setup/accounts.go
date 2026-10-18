// Package setup selects service providers for the development web and cleanup commands.
// Framework applications choose and initialize their own providers.
package setup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/config"
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

// NeedsSQLPool reports whether the selected account provider uses the shared pool.
// Other providers open and own their resources through their configured factories.
func (s *AccountStorage) NeedsSQLPool() bool {
	return s.config.Auth.Enabled && (s.config.Auth.AccountStorage.Provider == "" || s.config.Auth.AccountStorage.Provider == "sqlite")
}

// AccountProvider is the reference commands' account adapter contract.
type AccountProvider = user.StorageProvider

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
func (s *AccountStorage) Open(ctx context.Context, pool *sql.DB) (user.AccountRepository, func() error, error) {
	if !s.config.Auth.Enabled {
		return nil, nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, accountSetupTimeout)
	defer cancel()
	selection, providers := accountSelection(s.config, pool, s.providers)
	return user.OpenConfiguredStorage(ctx, selection, providers)
}

// accountSelection supplies the reference commands' default adapter without I/O.
func accountSelection(cfg *config.Config, pool *sql.DB, available map[string]AccountProvider) (config.AccountStorageConfig, map[string]user.StorageProvider) {
	selection := cfg.Auth.AccountStorage
	if selection.Provider == "" {
		selection.Provider = "sqlite"
	}
	providers := maps.Clone(available)
	if providers == nil {
		providers = make(map[string]user.StorageProvider)
	}
	providers["sqlite"] = usersqlite.ConfiguredProvider(*cfg, pool)
	return selection, providers
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

func cleanupAccountTokens(ctx context.Context, cfg *config.Config, limit int, providers map[string]AccountProvider) (int64, error) {
	if limit < 1 {
		return 0, errors.New("token cleanup limit must be positive")
	}
	if !cfg.Auth.Enabled {
		return 0, nil
	}
	selection, available := accountSelection(cfg, nil, providers)
	return user.CleanupConfiguredTokens(ctx, selection, available, limit)
}
