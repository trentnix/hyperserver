package logger

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
)

type recordingLogger struct {
	Logger
	fields   []Field
	messages []string
}

func (l *recordingLogger) With(fields ...Field) Logger {
	l.fields = append(l.fields, fields...)
	return l
}

func (l *recordingLogger) Error(message string, fields ...Field) {
	l.messages = append(l.messages, message)
	l.fields = append(l.fields, fields...)
}

func TestLogRequestErrorWithoutLogger(t *testing.T) {
	err := errors.New("test failure")
	LogRequestError(nil, err)
	r := httptest.NewRequest("GET", "/", nil)
	LogRequestError(r, err)
	LogRequestError(r.WithContext(Set(r.Context(), nil)), err)
	var missing Logger
	LogRequestError(r.WithContext(Set(r.Context(), &missing)), err)
}

func TestLogRequestError(t *testing.T) {
	recorded := &recordingLogger{}
	var l Logger = recorded
	r := httptest.NewRequest("POST", "/test", nil)
	ctx := context.WithValue(r.Context(), RequestIDKey, "request-123")
	r = r.WithContext(Set(ctx, &l))
	LogRequestError(r, nil)
	if len(recorded.messages) != 0 || len(recorded.fields) != 0 {
		t.Fatal("nil error produced a log entry")
	}
	LogRequestError(r, fmt.Errorf("load failed: %w", errors.New("storage unavailable")))
	if len(recorded.messages) != 1 || recorded.messages[0] != "Request Error" {
		t.Fatalf("messages = %v", recorded.messages)
	}
	fields := make(map[string]any)
	for _, field := range recorded.fields {
		fields[field.Key] = field.Value
	}
	for key, want := range map[string]string{"requestID": "request-123", "method": "POST", "path": "/test", "error": "load failed: storage unavailable -> storage unavailable"} {
		if fields[key] != want {
			t.Errorf("field %s = %v, want %q", key, fields[key], want)
		}
	}
}
