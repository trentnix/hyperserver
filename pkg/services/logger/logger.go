// logger.go defines the Logger interface that is used by the application. The interface
// will make it easier to replace the logger if necessary
package logger

import (
	"context"
	"net/http"
)

type (
	contextKey string
)

const (
	LoggerContextKey = contextKey("logger")
	RequestIDKey     = contextKey("requestID")
)

// Logger is a custom logging interface.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
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

// LogRequestError writes an error log entry using the provided request, error message, error
// instance, and http status value. Even though the same logger is retrieved from each context,
// using .With guarantees thread safety.
func LogRequestError(r *http.Request, err error) {
	if r == nil {
		return
	}

	ctxLogger := *Get(r.Context())

	requestID, _ := r.Context().Value(RequestIDKey).(string)
	errorLogger := ctxLogger.With(
		Field{Key: string(RequestIDKey), Value: requestID},
		Field{Key: "method", Value: r.Method},
		Field{Key: "path", Value: r.URL.Path},
		Field{Key: "error", Value: err},
	)
	errorLogger.Error("Request Error")
}
