package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// LoggerMiddleware logs the request method and path, excluding query parameters.
// It adds a logger and tracing ID to the request context and returns the ID in
// X-Request-ID. Handlers receive the original request URL unchanged.
func LoggerMiddleware(l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestId := getRequestIdentifier()
			ctx := context.WithValue(r.Context(), logger.RequestIDKey, requestId)
			if l != nil {
				reqLogger := l.With(
					slog.Any(string(logger.RequestIDKey), requestId),
					slog.Any("method", r.Method),
					slog.Any("path", r.URL.Path),
				)

				ctx = logger.Set(ctx, reqLogger)
				reqLogger.InfoContext(ctx, "Incoming request")
			} else {
				ctx = logger.Set(ctx, nil)
			}

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
