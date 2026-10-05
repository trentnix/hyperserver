// Package handlers holds the process-wide registry of application modules.
// Modules typically call Register during package initialization. Registration does
// not initialize a module or bind its routes, and the registry stores shared instances.
package handlers

import (
	"context"

	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

// handlers provides a global instance of Handlers that can be used in the application
var handlers []Handler

type (
	// Handler defines an interface that can be used to add routes and handlers to
	// a HyperServer application
	Handler interface {
		// Routes binds the initialized module's HTTP routes. Registration errors
		// must stop startup before accepting traffic.
		Routes(*routing.Routes) error
		// Init prepares the module before activation. Shared services are borrowed,
		// and the module must not close them. Use ctx for initialization I/O.
		Init(context.Context, *server.ApplicationServer) error
	}
)

// Register appends a shared module instance to the process-wide registry.
// Call it during startup, before reading the registry or serving requests.
func Register(h Handler) {
	handlers = append(handlers, h)
}

// GetHandlers returns the registry's backing slice, not a copy.
// Callers must not modify it while the application is running.
func GetHandlers() []Handler {
	return handlers
}
