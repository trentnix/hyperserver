package auth

import (
	"context"
	"fmt"
	"slices"

	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

// Module describes authentication with an explicit provider catalog. Each instance
// owns its manager and enabled providers. An empty catalog does not use imports.
// Required account, session, mail, and rendering services must be ready before Init.
func Module(providers []Descriptor) handlers.Descriptor {
	providers = slices.Clone(providers)
	return handlers.Descriptor{Name: "auth", New: func() handlers.Handler {
		return &module{providers: providers}
	}}
}

type module struct {
	manager   AuthManager
	providers []Descriptor
}

func init() {
	// Snapshot providers when an application creates the module, after all Go
	// package initialization has finished registering available providers.
	handlers.Register(handlers.Descriptor{Name: "auth", New: func() handlers.Handler {
		return &module{providers: Registered()}
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
		if app.Config.Auth.RegistrationEnabled && app.Config.Auth.RegisterRequiresVerification {
			validator, ok := provider.(VerificationConfigValidator)
			if !ok {
				return fmt.Errorf("auth.registerRequiresVerification requires a verification mechanism for auth service %q", provider.AuthType())
			}
			if err := validator.ValidateVerification(); err != nil {
				return fmt.Errorf("auth.registerRequiresVerification for auth service %q: %w", provider.AuthType(), err)
			}
		}
	}
	return nil
}

func (m *module) Routes(routes *routing.Routes) error {
	if err := m.manager.Routes(routes); err != nil {
		return err
	}
	for _, provider := range m.manager.Services.Services() {
		if err := provider.Routes(routes); err != nil {
			return fmt.Errorf("register auth service %q routes: %w", provider.AuthType(), err)
		}
	}
	return nil
}
