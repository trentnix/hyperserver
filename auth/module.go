package auth

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Module supplies the manager's accounts, sessions, and an explicit provider catalog.
// Configure provider dependencies through their own factories. Each instance
// owns its manager and enabled providers. An empty catalog does not use imports.
// Required account, session, mail, and rendering services must be ready before Init.
// Providers only configure themselves. Application setup owns service cleanup
// after failed startup or after HTTP requests drain, not the auth module.
func Module(accounts user.AccountRepository, sessions *session.SessionManager, providers []Descriptor) handlers.Descriptor {
	providers = slices.Clone(providers)
	return handlers.Descriptor{Name: "auth", New: func() handlers.Handler {
		return &module{manager: NewAuthManager(accounts, sessions), providers: providers}
	}}
}

type module struct {
	manager   *AuthManager
	providers []Descriptor
}

func init() {
	// Snapshot providers when an application creates the module, after all Go
	// package initialization has finished registering available providers.
	handlers.Register(handlers.Descriptor{Name: "auth", New: func() handlers.Handler {
		return &module{manager: NewAuthManager(nil, nil), providers: Registered()}
	}})
}

func (m *module) Init(ctx context.Context, app *server.ApplicationServer) error {
	registry, err := NewRegistry(app.Config, m.providers)
	if err != nil {
		return err
	}
	m.manager.Services = registry
	if err := m.manager.Init(ctx, app); err != nil {
		return err
	}

	for _, provider := range registry.Services() {
		if err := provider.Init(app); err != nil {
			return fmt.Errorf("initialize auth service %q: %w", provider.AuthType(), err)
		}
		_, registers := provider.(RegistrationHandler)
		if registers && app.Config.Auth.RegistrationEnabled && app.Config.Auth.RegisterRequiresVerification {
			validator, ok := provider.(VerificationConfigValidator)
			if !ok {
				return fmt.Errorf("auth.registerRequiresVerification requires a verification mechanism for auth service %q", provider.AuthType())
			}
			if err := validator.ValidateVerification(); err != nil {
				return fmt.Errorf("auth.registerRequiresVerification for auth service %q: %w", provider.AuthType(), err)
			}
		}
	}
	if log := logger.Get(ctx); log != nil {
		var providers []string
		for _, provider := range registry.Services() {
			providers = append(providers, provider.AuthType())
		}
		log.InfoContext(ctx, "Authentication providers initialized", slog.Any("providers", providers))
	}
	return nil
}

func (m *module) Routes(routes *routing.Routes) error {
	if err := m.manager.Routes(routes); err != nil {
		return err
	}
	for _, provider := range m.manager.Services.Services() {
		binder, ok := provider.(ProviderRoutes)
		if !ok {
			continue
		}
		if err := binder.Routes(routes); err != nil {
			return fmt.Errorf("register auth service %q routes: %w", provider.AuthType(), err)
		}
		if err := routes.Err(); err != nil {
			return fmt.Errorf("register auth service %q routes: %w", provider.AuthType(), err)
		}
	}
	return routes.Err()
}
