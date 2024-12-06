package handlers

import (
	"net/http"

	"github.com/trentnix/hyperserver/server"
)

var handlers []Handler

type (
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
