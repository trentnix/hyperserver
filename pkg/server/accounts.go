package server

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

const accountSetupTimeout = 10 * time.Second

// AccountProvider supplies account storage and optional token maintenance.
// Factories must honor ctx and validate their private copy of options without
// exposing secrets in errors. Each optional close function releases only owned
// resources, never borrowed pools. The application calls close on setup failure
// even when the factory returns an error. Otherwise it closes account storage at
// Shutdown and maintenance storage after each batch.
type AccountProvider struct {
	// Open creates a ready-to-use repository. It may initialize storage.
	Open func(ctx context.Context, options map[string]string) (user.AccountRepository, func() error, error)
	// OpenMaintenance opens existing storage without creating or migrating tables.
	// Nil means that this provider does not support explicit token maintenance.
	OpenMaintenance func(ctx context.Context, options map[string]string) (user.TokenMaintenance, func() error, error)
}

// RegisterAccountProvider adds a provider to this application. Names are
// case-sensitive. The built-in name "sqlite" is reserved. Register serially
// before initialization or maintenance. Duplicate names and nil Open factories are rejected.
func (s *ApplicationServer) RegisterAccountProvider(name string, provider AccountProvider) error {
	if name == "" || strings.TrimSpace(name) != name {
		return errors.New("account provider name must not be empty or have surrounding whitespace")
	}
	if provider.Open == nil {
		return fmt.Errorf("account provider %q has no factory", name)
	}
	if s.AccountRepository != nil {
		return errors.New("account providers must be registered before account initialization")
	}
	if _, exists := s.accountProviders[name]; name == "sqlite" || exists {
		return fmt.Errorf("account provider %q is already registered", name)
	}
	if s.accountProviders == nil {
		s.accountProviders = make(map[string]AccountProvider)
	}
	s.accountProviders[name] = provider
	return nil
}

// InitializeAccounts prepares the selected provider when authentication is enabled.
// It preserves an already supplied repository, bypassing provider selection.
// Call serially before modules and requests use accounts. Failed initialization
// leaves AccountRepository unset and can be retried. SQLite borrows Database.
func (s *ApplicationServer) InitializeAccounts(ctx context.Context) error {
	if !s.Config.Auth.Enabled || s.AccountRepository != nil {
		return nil
	}
	name, provider, err := s.accountProvider()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, accountSetupTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	repository, cleanup, err := provider.Open(ctx, maps.Clone(s.Config.Auth.AccountStorage.Options))
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
		return fmt.Errorf("initialize account provider %q: %w", name, err)
	}
	s.AccountRepository, s.closeAccounts = repository, cleanup
	return nil
}

func (s *ApplicationServer) initializeSQLiteAccounts(ctx context.Context, options map[string]string) (user.AccountRepository, func() error, error) {
	if err := user.ValidateSQLiteAccountOptions(options); err != nil {
		return nil, nil, err
	}
	repository := user.NewSQLiteAccountRepository(s.Database)
	if err := repository.Initialize(ctx); err != nil {
		return nil, nil, err
	}
	return repository, nil, nil
}

func (s *ApplicationServer) accountProvider() (string, AccountProvider, error) {
	name := s.Config.Auth.AccountStorage.Provider
	if name == "" {
		name = "sqlite"
	}
	if name == "sqlite" {
		return name, AccountProvider{
			Open: s.initializeSQLiteAccounts,
			OpenMaintenance: func(ctx context.Context, options map[string]string) (user.TokenMaintenance, func() error, error) {
				return user.OpenSQLiteTokenMaintenance(ctx, s.Config.Database, options)
			},
		}, nil
	}
	provider, ok := s.accountProviders[name]
	if !ok {
		return name, AccountProvider{}, fmt.Errorf("auth.accountStorage.provider: unknown provider %q", name)
	}
	return name, provider, nil
}

// CleanupAccountTokens removes one bounded batch through the selected provider's
// maintenance capability and closes resources opened for that batch. It does not
// initialize account storage. A supplied AccountRepository must implement
// user.TokenMaintenance. Maintenance does not close that repository's resources.
func (s *ApplicationServer) CleanupAccountTokens(ctx context.Context, limit int) (count int64, err error) {
	if limit < 1 {
		return 0, errors.New("token cleanup limit must be positive")
	}
	if !s.Config.Auth.Enabled {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s.AccountRepository != nil {
		maintenance, ok := s.AccountRepository.(user.TokenMaintenance)
		if !ok {
			return 0, errors.New("supplied account repository does not support token maintenance")
		}
		return maintenance.DeleteExpiredTokens(ctx, limit)
	}
	name, provider, err := s.accountProvider()
	if err != nil {
		return 0, err
	}
	if provider.OpenMaintenance == nil {
		return 0, fmt.Errorf("account provider %q does not support token maintenance", name)
	}
	maintenance, closeStore, err := provider.OpenMaintenance(ctx, maps.Clone(s.Config.Auth.AccountStorage.Options))
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
