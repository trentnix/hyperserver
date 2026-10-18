package routing

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
)

func TestApplicationRoutesRejectInvalidDefault(t *testing.T) {
	mux := http.NewServeMux()
	if routes, err := NewConfiguredRoutes(mux, config.HTTPConfig{DefaultRateLimit: config.RateLimitConfig{Enabled: true}}, nil); routes != nil || err == nil {
		t.Fatal("invalid default accepted")
	}
	if _, pattern := mux.Handler(httptest.NewRequest("GET", "/", nil)); pattern != "" {
		t.Fatal("failed setup registered a route")
	}
}

func TestConfiguredRoutesRequireMux(t *testing.T) {
	if routes, err := NewConfiguredRoutes(nil, config.HTTPConfig{}, nil); routes != nil || err == nil {
		t.Fatal("nil mux accepted")
	}
}

// Capture warnings and ignore the informational startup summary.
type ratePolicyWarningLogger struct {
	slog.Handler
	warnings []map[string]any
}

func (*ratePolicyWarningLogger) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}
func (l *ratePolicyWarningLogger) Handle(_ context.Context, record slog.Record) error {
	entry := map[string]any{"message": record.Message}
	record.Attrs(func(attr slog.Attr) bool { entry[attr.Key] = attr.Value.Any(); return true })
	l.warnings = append(l.warnings, entry)
	return nil
}

func TestApplicationRatePolicyWarnings(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		requests                   int
		window                     time.Duration
		defaultPolicy, optOut      bool
		disabled, noLogger, warned bool
	}{
		{name: "higher route", requests: 200, window: time.Minute, warned: true},
		{name: "higher default", requests: 200, window: time.Minute, defaultPolicy: true, warned: true},
		{name: "lower route replaces higher default", requests: 10, window: time.Minute},
		{name: "equal", requests: 20, window: time.Minute},
		{name: "longer window", requests: 200, window: time.Hour},
		{name: "shorter window", requests: 200, window: time.Second},
		{name: "opt out", requests: 200, window: time.Minute, optOut: true},
		{name: "shared disabled", requests: 200, window: time.Minute, disabled: true},
		{name: "no logger", requests: 200, window: time.Minute, noLogger: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.HTTPConfig{
				DefaultRateLimit: config.RateLimitConfig{Enabled: true, Requests: 200, Window: time.Minute, MaxClients: 10},
				SharedRateLimit:  config.RateLimitConfig{Enabled: !tc.disabled, Requests: 20, Window: time.Minute, MaxClients: 10},
			}
			capture := &ratePolicyWarningLogger{}
			log := slog.New(capture)
			if tc.noLogger {
				log = nil
			}
			mux := http.NewServeMux()
			routes, err := NewConfiguredRoutes(mux, cfg, log)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.defaultPolicy {
				routes, err = routes.WithRateLimit(ratelimit.Policy{Requests: tc.requests, Window: tc.window, MaxClients: 10})
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc.optOut {
				routes = routes.WithoutRateLimit()
			}
			routes.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			if !tc.warned {
				if len(capture.warnings) != 0 {
					t.Fatalf("unexpected warnings: %v", capture.warnings)
				}
				return
			}
			if len(capture.warnings) != 1 {
				t.Fatalf("warnings = %v, want one startup warning", capture.warnings)
			}
			entry := capture.warnings[0]
			if entry["route"] != "GET /items/{id}" || entry["routeRequests"] != int64(200) || entry["sharedRequests"] != int64(20) || entry["window"] != "1m0s" || !strings.Contains(entry["message"].(string), "per client IP") {
				t.Fatalf("incomplete warning: %v", entry)
			}
			for i := 0; i < 2; i++ {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest("GET", "/items/private-value", nil))
				if w.Code != 204 || len(capture.warnings) != 1 {
					t.Fatal("startup warning blocked the route or repeated during requests")
				}
			}
		})
	}
}
