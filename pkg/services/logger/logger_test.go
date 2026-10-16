package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
)

func TestContextLogger(t *testing.T) {
	parent := context.Background()
	if Get(parent) != nil {
		t.Fatal("empty context returned a logger")
	}
	first := slog.New(slog.NewTextHandler(io.Discard, nil))
	second := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := Set(parent, first)
	child := Set(ctx, second)
	if Get(ctx) != first || Get(child) != second || Get(parent) != nil {
		t.Fatal("context logger ownership changed")
	}
	if Get(Set(child, nil)) != nil || Get(child) != second {
		t.Fatal("nil did not mask the inherited logger")
	}
}

func TestLogRequestErrorWithoutLogger(t *testing.T) {
	failure := errors.New("test failure")
	LogRequestError(nil, failure)
	r := httptest.NewRequest("GET", "/", nil)
	LogRequestError(r, failure)
	LogRequestError(r.WithContext(Set(r.Context(), nil)), failure)
}

func TestLogRequestError(t *testing.T) {
	var output bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&output, nil)).With("requestID", "request-123")
	r := httptest.NewRequest("POST", "/test", nil)
	r = r.WithContext(Set(r.Context(), l))
	LogRequestError(r, nil)
	if output.Len() != 0 {
		t.Fatal("nil error produced a record")
	}
	LogRequestError(r, fmt.Errorf("load failed: %w", errors.New("storage unavailable")))
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["requestID"] != "request-123" || record["error"] != "load failed: storage unavailable -> storage unavailable" {
		t.Fatalf("lost inherited fields or error cause: %v", record)
	}
}
