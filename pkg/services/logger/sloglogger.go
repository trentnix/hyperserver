package logger

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"time"
)

// SlogLogger adapts a slog.Handler to the framework's Logger contract.
// The handler controls formatting, filtering, and optional redaction.
type SlogLogger struct {
	handler slog.Handler
}

// NewSlogLogger creates a logger using handler. Nil selects JSON on stderr at
// Info level. It does not change slog's process-wide default logger.
func NewSlogLogger(handler slog.Handler) *SlogLogger {
	if handler == nil {
		handler = slog.NewJSONHandler(os.Stderr, nil)
	}
	return &SlogLogger{handler: handler}
}

// Debug writes a diagnostic message and structured fields.
func (l *SlogLogger) Debug(msg string, fields ...Field) {
	l.log(slog.LevelDebug, msg, fields)
}

// Info writes an informational message and structured fields.
func (l *SlogLogger) Info(msg string, fields ...Field) {
	l.log(slog.LevelInfo, msg, fields)
}

// Warn writes a warning and structured fields.
func (l *SlogLogger) Warn(msg string, fields ...Field) {
	l.log(slog.LevelWarn, msg, fields)
}

// Error writes an error message and structured fields.
func (l *SlogLogger) Error(msg string, fields ...Field) {
	l.log(slog.LevelError, msg, fields)
}

// With returns an independent logger with additional structured fields.
func (l *SlogLogger) With(fields ...Field) Logger {
	return &SlogLogger{handler: l.handler.WithAttrs(toSlogAttrs(fields))}
}

func (l *SlogLogger) log(level slog.Level, msg string, fields []Field) {
	ctx := context.Background()
	if !l.handler.Enabled(ctx, level) {
		return
	}

	// Attribute optional source information to the caller, not this adapter.
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:]) // Skip Callers, log, and the level-specific method.
	record := slog.NewRecord(time.Now(), level, msg, pcs[0])
	record.AddAttrs(toSlogAttrs(fields)...)
	_ = l.handler.Handle(ctx, record) // Logger has no error return, matching slog.Logger.
}

func toSlogAttrs(fields []Field) []slog.Attr {
	attrs := make([]slog.Attr, len(fields))
	for i, field := range fields {
		attrs[i] = slog.Any(field.Key, field.Value)
	}
	return attrs
}
