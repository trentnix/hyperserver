package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

func TestWebStack(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	cfg := config.HTTPConfig{TrustedProxies: []string{"192.0.2.1/32"}, HSTSMaxAge: 300,
		SharedRateLimit: config.RateLimitConfig{Enabled: true, Requests: 2, Window: time.Hour, MaxClients: 10}}
	calls := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !requestinfo.IsHTTPS(r) || logger.Get(r.Context()) == nil {
			t.Error("inner handler did not receive verified HTTPS and logging")
		}
		w.WriteHeader(204)
	})
	handler, err := Web(inner, cfg, "default-src 'none'", log)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, scheme, origin string
		status               int
		called               bool
	}{
		{"invalid proxy", "", "", 400, false},
		{"cross-origin", "https", "https://other.test", 403, false},
		{"same-origin", "https", "https://example.test", 204, true},
		{"limited", "https", "https://example.test", 429, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://example.test/", nil)
			r.RemoteAddr = "192.0.2.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.1")
			r.Header.Set("X-Forwarded-Proto", test.scheme)
			r.Header.Set("Origin", test.origin)
			before := calls
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.status || (calls > before) != test.called {
				t.Fatalf("status=%d calls=%d, want %d called=%v", w.Code, calls-before, test.status, test.called)
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") != "default-src 'none'" || w.Header().Get("X-Request-ID") == "" {
				t.Fatal("response lacks outer security headers or request ID")
			}
			if test.scheme != "" && w.Header().Get("Strict-Transport-Security") != "max-age=300" {
				t.Fatal("verified HTTPS rejection lost HSTS")
			}
		})
	}
	if strings.Count(logs.String(), "Request completed") != 4 {
		t.Fatal("rejected requests were not logged")
	}
}

func TestWebInvalidConfiguration(t *testing.T) {
	for _, cfg := range []config.HTTPConfig{
		{TrustedProxies: []string{"invalid"}}, {HSTSMaxAge: -1},
		{SharedRateLimit: config.RateLimitConfig{Enabled: true}},
	} {
		if handler, err := Web(http.NewServeMux(), cfg, "", nil); handler != nil || err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	if handler, err := Web(nil, config.HTTPConfig{}, "", nil); handler != nil || err == nil {
		t.Fatal("nil handler accepted")
	}
}
