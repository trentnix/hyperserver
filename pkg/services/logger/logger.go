// logger.go defines the Logger interface that is used by the application. The interface
// will make it easier to replace the logger if necessary
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

// LogRequestError writes a log entry using the provided request and error. Using
// .With guarantees thread safety.
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
