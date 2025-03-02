// modules.go is how modules get registered (by being imported anonymously)
package main

import (
	"github.com/trentnix/hyperserver/auth"
	_ "github.com/trentnix/hyperserver/auth"
	_ "github.com/trentnix/hyperserver/auth/modules/email"
	"github.com/trentnix/hyperserver/pkg/server"
)

// Setup builds the router by iterating over the handlers registered with the
// application and calling their Routes method
func SetupAuthServices(s *server.ApplicationServer) error {
	// Initialize and register all handlers
	for _, a := range auth.GetAuthServices() {
		if err := a.Init(s); err != nil {
			return err
		}

		a.Routes(s.Web)
	}

	return nil
}
