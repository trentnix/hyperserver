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

// AccountProvider validates options and creates a ready-to-use repository.
// It must honor ctx. Options are a private copy of the selected configuration.
// Errors must not include secrets from options.
// The optional cleanup function releases only resources the provider owns, never
// borrowed pools. The application calls cleanup on setup failure (even when err
// is non-nil), or during Shutdown after consumers stop. If cleanup is nil, the
// provider must release resources itself on failure or leave ownership with its caller.
type AccountProvider func(ctx context.Context, options map[string]string) (repository user.AccountRepository, cleanup func() error, err error)

// RegisterAccountProvider adds a factory to this application. Names are
// case-sensitive. The built-in name "sqlite" is reserved. Register serially
// before InitializeAccounts. Duplicate names and nil factories are rejected.
func (s *ApplicationServer) RegisterAccountProvider(name string, provider AccountProvider) error {
	if name == "" || strings.TrimSpace(name) != name {
		return errors.New("account provider name must not be empty or have surrounding whitespace")
	}
	if provider == nil {
		return fmt.Errorf("account provider %q has no factory", name)
	}
	if s.AccountRepository != nil {
		return errors.New("account providers must be registered before account initialization")
	}
	if name == "sqlite" || s.accountProviders[name] != nil {
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
	settings := s.Config.Auth.AccountStorage
	name := settings.Provider
	if name == "" {
		name = "sqlite"
	}
	provider := s.accountProviders[name]
	if name == "sqlite" {
		provider = s.initializeSQLiteAccounts
	}
	if provider == nil {
		return fmt.Errorf("auth.accountStorage.provider: unknown provider %q", name)
	}

	ctx, cancel := context.WithTimeout(ctx, accountSetupTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	repository, cleanup, err := provider(ctx, maps.Clone(settings.Options))
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
	if len(options) != 0 {
		return nil, nil, errors.New("auth.accountStorage.options: sqlite uses database configuration and accepts no options")
	}
	repository := user.NewSQLiteAccountRepository(s.Database)
	if err := repository.Initialize(ctx); err != nil {
		return nil, nil, err
	}
	return repository, nil, nil
}
