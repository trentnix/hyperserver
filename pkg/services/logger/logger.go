// Package logger attaches standard slog loggers to request contexts and logs errors.
// Callers must keep secrets out of log fields and error messages.
package logger

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

type contextKey string

const (
	// LoggerContextKey identifies the request's *slog.Logger.
	LoggerContextKey = contextKey("logger")
	// RequestIDKey identifies the tracing ID assigned by request middleware.
	RequestIDKey = contextKey("requestID")
)

// Set returns a context containing l. Nil masks an inherited logger.
func Set(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, LoggerContextKey, l)
}

// Get returns the context's logger, or nil when none is set.
func Get(ctx context.Context) *slog.Logger {
	l, _ := ctx.Value(LoggerContextKey).(*slog.Logger)
	return l
}

// LogRequestError logs private error details through the request's logger.
// Request middleware supplies correlation fields. A missing logger is ignored.
func LogRequestError(r *http.Request, err error) {
	if r == nil || err == nil {
		return
	}
	l := Get(r.Context())
	if l == nil {
		return
	}

	var messages []string
	for err != nil {
		messages = append(messages, err.Error())
		err = errors.Unwrap(err)
	}
	l.ErrorContext(r.Context(), "Request Error", "error", strings.Join(messages, " -> "))
}
