// handlers.go defines the Handler interface and also provides functions for registering a
// Handler instance as well as getting all of the registered Handler instances
package handlers

import (
	"net/http"

	"github.com/trentnix/hyperserver/server"
)

// handlers provides a global instance of Handlers that can be used in the application
var handlers []Handler

type (
	// Handler defines an interface that can be used to add routes and handlers to
	// a HyperServer application
	Handler interface {
		Routes(*http.ServeMux)
		Init(*server.ApplicationServer) error
	}
)

// Register used by a Handler to register itself with the application
func Register(h Handler) {
	handlers = append(handlers, h)
}

// GetHandlers retrieves the handlers that have been registered with the application
func GetHandlers() []Handler {
	return handlers
}
