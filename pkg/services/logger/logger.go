// Package logger defines structured logging and request-context helpers.
// ZapLogger implements Logger. Callers must avoid logging secrets or account tokens.
package logger

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type (
	contextKey string
)

const (
	// LoggerContextKey identifies a *Logger stored in a context.
	LoggerContextKey = contextKey("logger")
	// RequestIDKey identifies the tracing ID assigned by request logging middleware.
	RequestIDKey = contextKey("requestID")
)

// Logger is a custom logging interface.
type Logger interface {
	// Debug writes a diagnostic message and structured fields.
	Debug(msg string, fields ...Field)
	// Info writes an informational message and structured fields.
	Info(msg string, fields ...Field)
	// Warn writes a warning and structured fields.
	Warn(msg string, fields ...Field)
	// Error writes an error message and structured fields.
	Error(msg string, fields ...Field)
	// With returns a logger that includes fields in subsequent entries.
	With(fields ...Field) Logger
}

// Field represents a logging field.
type Field struct {
	Key   string
	Value interface{}
}

// Set adds the specified logger to the specified context.Context and returns it
func Set(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, LoggerContextKey, l)
}

// Get retrieves the logger instance from the specified context
func Get(ctx context.Context) *Logger {
	logger, ok := ctx.Value(LoggerContextKey).(*Logger)
	if !ok || logger == nil {
		return nil
	}

	return logger
}

// LogRequestError logs the error chain and request metadata.
// A nil request is ignored. A non-nil request must have a Logger in its context.
// Errors must not contain secrets because their messages are included verbatim.
func LogRequestError(r *http.Request, err error) {
	if r == nil {
		return
	}

	// recursively unwrap the error to build a full error message the underlying error (if any)
	var msgs []string
	for err != nil {
		msgs = append(msgs, err.Error())
		err = errors.Unwrap(err)
	}

	// join messages with a separator (e.g., " -> ") to show the chain.
	errMessage := strings.Join(msgs, " -> ")

	ctxLogger := *Get(r.Context())
	requestID, _ := r.Context().Value(RequestIDKey).(string)

	errorLogger := ctxLogger.With(
		Field{Key: string(RequestIDKey), Value: requestID},
		Field{Key: "method", Value: r.Method},
		Field{Key: "path", Value: r.URL.Path},
		Field{Key: "error", Value: errMessage},
	)

	errorLogger.Error("Request Error")
}
