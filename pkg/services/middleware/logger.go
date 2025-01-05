// logger.go defines middleware that relates to
package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// LoggerMiddleware logs the details of a request, adds a logger instance to the
// request context, and created a request identifier so that a request can be followed
// through the application via the logs
func LoggerMiddleware(l logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestId := getRequestIdentifier()
			ctx := context.WithValue(r.Context(), logger.RequestIDKey, requestId)
			ctx = logger.Set(ctx, &l)

			// Add request-specific fields if needed
			reqLogger := l.With(
				logger.Field{Key: string(logger.RequestIDKey), Value: requestId},
				logger.Field{Key: "method", Value: r.Method},
				logger.Field{Key: "path", Value: r.URL.Path},
			)

			reqLogger.Info("Incoming request")

			// set it in the response header for client visibility
			w.Header().Set("X-Request-ID", requestId)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// getRequestIdentifier returns a newly created UUID value that will serve as a request
// identifier for tracing requests through the application
func getRequestIdentifier() string {
	return uuid.New().String()
}
