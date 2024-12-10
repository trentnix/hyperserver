// middleware.go defines the general functions that facilitate the middleware used in
// the application
package middleware

import "net/http"

// ChainMiddleware allows for multiple middleware methods to be used when a given
// request is processed
func ChainMiddleware(handler http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for _, middleware := range middlewares {
		handler = middleware(handler)
	}

	return handler
}
