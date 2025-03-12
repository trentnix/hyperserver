// modules.go is how modules get registered (by being imported anonymously)
package main

import (
	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/server"

	// AuthService implementations - required so that init() is run in each so they
	// can self-register
	_ "github.com/trentnix/hyperserver/auth/modules/email"
)

// SetupAuthentication initializes any registered authentication services and adds
// their routes to the
func SetupAuthentication(s *server.ApplicationServer) error {
	if s.Config.Auth.Enabled {
		// initialize and register all handlers
		for _, a := range auth.GetAuthServices() {
			if err := a.Init(s); err != nil {
				return err
			}

			a.Routes(s.Web)
		}
	}

	return nil
}
