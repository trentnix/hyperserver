// modules.go is how modules get registered (by being imported anonymously) and
// initialized via SetupHandlers
package main

import (
	"github.com/trentnix/hyperserver/handlers"
	_ "github.com/trentnix/hyperserver/modules/site"
	"github.com/trentnix/hyperserver/server"
)

// SetupHandlers iterates over the Handler instances that have self-registered, calls
// Init to initalize each Handler, and calls each Handler instance's Routes method to
// register route handlers with the Application Server's router
func SetupHandlers(s *server.ApplicationServer) error {
	for _, h := range handlers.GetHandlers() {
		if err := h.Init(s); err != nil {
			return err
		}

		h.Routes(s.Web)
	}

	return nil
}
