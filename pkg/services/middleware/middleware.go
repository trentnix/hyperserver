// Package middleware composes net/http handlers for logging, sessions, and authentication.
// ChainMiddleware runs the last supplied middleware first. Authentication helpers
// require database, content-manager, and session dependencies to be configured.
package middleware

import "net/http"

// ChainMiddleware wraps handler in each supplied middleware.
// The last middleware in the argument list runs first for incoming requests.
func ChainMiddleware(handler http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for _, middleware := range middlewares {
		handler = middleware(handler)
	}

	return handler
}
