// handlers.go initializes registered handlers
package main

import (
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
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
