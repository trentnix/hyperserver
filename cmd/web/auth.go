// modules.go is how modules get registered (by being imported anonymously)
package main

import (
	"fmt"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"

	// AuthService implementations - required so that init() is run in each so they
	// can self-register
	_ "github.com/trentnix/hyperserver/auth/modules/email"
)

// SetupAuthentication initializes enabled authentication providers, validates
// required verification, and registers provider routes.
func SetupAuthentication(s *server.ApplicationServer) error {
	if err := auth.ValidateConfig(s.Config); err != nil {
		return err
	}
	if s.Config.Auth.Enabled {
		authServices := make([]auth.AuthService, len(auth.GetAuthServices()))
		copy(authServices, auth.GetAuthServices())

		// initialize and register all handlers
		for _, a := range authServices {
			options := auth.GetAuthConfigOptions(s.Config, a.AuthType())
			enabled, err := config.ProviderEnabled("auth.services."+a.AuthType()+".enabled", options["enabled"])
			if err != nil {
				return err
			}
			if !enabled {
				auth.RemoveAuthService(a.AuthType())
				continue
			}
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
			a.Routes(s.Web)
		}

		if len(auth.GetAuthServices()) == 0 {
			return fmt.Errorf("auth is enabled but no auth services are configured")
		}
	}

	return nil
}
