package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

type logContextKey struct{}
type contextCheckingHandler struct {
	slog.Handler
	t *testing.T
}

func (h contextCheckingHandler) Handle(ctx context.Context, record slog.Record) error {
	if ctx.Value(logContextKey{}) != "retained" {
		h.t.Error("lost request context")
	}
	return h.Handler.Handle(ctx, record)
}
func (h contextCheckingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextCheckingHandler{h.Handler.WithAttrs(attrs), h.t}
}

func TestLoggerMiddlewareEnrichesHandlerLogs(t *testing.T) {
	var output bytes.Buffer
	l := slog.New(contextCheckingHandler{slog.NewJSONHandler(&output, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelWarn,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == "secret" {
				return slog.String("secret", "[redacted]")
			}
			return attr
		},
	}), t})
	handler := LoggerMiddleware(l)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Get(r.Context()).WarnContext(r.Context(), "handler warning", "secret", "private-value")
		logger.LogRequestError(r, errors.New("storage unavailable"))
	}))
	r := httptest.NewRequest("GET", "/page?token=private-token", nil)
	r = r.WithContext(context.WithValue(r.Context(), logContextKey{}, "retained"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if strings.Contains(output.String(), "private-") {
		t.Fatal("logged secrets")
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected warning and error, got %s", output.String())
	}
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{"requestID": w.Header().Get("X-Request-ID"), "method": "GET", "path": "/page"} {
			if want == "" || record[key] != want || strings.Count(line, `"`+key+`":`) != 1 {
				t.Fatalf("missing or duplicate %s: %s", key, line)
			}
		}
		if record["source"] == nil {
			t.Fatal("lost optional source location")
		}
		if record["msg"] == "handler warning" {
			source := record["source"].(map[string]any)
			if !strings.HasSuffix(source["file"].(string), "middleware/logger_test.go") || record["secret"] != "[redacted]" {
				t.Fatalf("wrong source or redaction: %v", record)
			}
		}
	}
}

func TestLoggerMiddlewareWithoutLogger(t *testing.T) {
	called := false
	handler := LoggerMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if logger.Get(r.Context()) != nil {
			t.Fatal("unexpected logger")
		}
		logger.LogRequestError(r, errors.New("ignored"))
		w.WriteHeader(http.StatusNoContent)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if !called || w.Code != 204 || w.Header().Get("X-Request-ID") == "" {
		t.Fatal("nil logger changed request handling")
	}
}
