package main

import (
	"fmt"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"

	// AuthService implementations - required so that init() is run in each so they
	// can self-register
	_ "github.com/trentnix/hyperserver/auth/modules/email"
)

// SetupAuthentication initializes enabled authentication providers, validates
// required verification, and registers provider routes.
func SetupAuthentication(s *server.ApplicationServer, log logger.Logger, registry *auth.Registry) error {
	if s.Config.Auth.Enabled {
		routes, err := applicationRoutes(s.Web, s.Config.HTTP, log)
		if err != nil {
			return err
		}
		authServices := registry.Services()

		// initialize and register all handlers
		for _, a := range authServices {
			if err := a.Init(s); err != nil {
				return fmt.Errorf("initialize auth service %q: %w", a.AuthType(), err)
			}

			if s.Config.Auth.RegistrationEnabled && s.Config.Auth.RegisterRequiresVerification {
				validator, ok := a.(auth.VerificationConfigValidator)
				if !ok {
					return fmt.Errorf("auth.registerRequiresVerification requires a verification mechanism for auth service %q", a.AuthType())
				}
				if err := validator.ValidateVerification(); err != nil {
					return fmt.Errorf("auth.registerRequiresVerification for auth service %q: %w", a.AuthType(), err)
				}
			}

			// register any custom routes the initialized auth service handles
			if err := a.Routes(routes); err != nil {
				return fmt.Errorf("register auth service %q routes: %w", a.AuthType(), err)
			}
		}

		if len(authServices) == 0 {
			return fmt.Errorf("auth is enabled but no auth services are configured")
		}
	}

	return nil
}
