// modules.go is how modules get registered (by being imported anonymously)
package main

import (
	"fmt"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/server"

	// AuthService implementations - required so that init() is run in each so they
	// can self-register
	_ "github.com/trentnix/hyperserver/auth/modules/email"
)

// SetupAuthentication initializes any registered authentication services and adds
// their routes to the
func SetupAuthentication(s *server.ApplicationServer) error {
	if err := auth.ValidateConfig(s.Config); err != nil {
		return err
	}
	if s.Config.Auth.Enabled {
		authServices := make([]auth.AuthService, len(auth.GetAuthServices()))
		copy(authServices, auth.GetAuthServices())

		// initialize and register all handlers
		for _, a := range authServices {
			if err := a.Init(s); err != nil {
				// the specified service didn't initialize - remove it from the registered auth services
				auth.RemoveAuthService(a.AuthType())
			} else {
				// register any custom routes the initialized auth service handles
				a.Routes(s.Web)
			}
		}

		if len(auth.GetAuthServices()) == 0 {
			return fmt.Errorf("auth is enabled but no auth services are configured")
		}
	}

	return nil
}
