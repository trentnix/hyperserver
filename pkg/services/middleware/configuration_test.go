package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
)

func TestFromConfigRejectsInvalidSettings(t *testing.T) {
	for _, test := range []struct {
		name, field string
		cfg         config.HTTPConfig
	}{
		{"proxy", "http.trustedProxies", config.HTTPConfig{TrustedProxies: []string{"invalid"}}},
		{"HSTS", "http.hstsMaxAge", config.HTTPConfig{HSTSMaxAge: -1}},
		{"requests", "http.sharedRateLimit", config.HTTPConfig{SharedRateLimit: config.RateLimitConfig{Enabled: true, Window: time.Minute, MaxClients: 10}}},
		{"window", "http.sharedRateLimit", config.HTTPConfig{SharedRateLimit: config.RateLimitConfig{Enabled: true, Requests: 1, MaxClients: 10}}},
		{"capacity", "http.sharedRateLimit", config.HTTPConfig{SharedRateLimit: config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, err := FromConfig(http.NewServeMux(), test.cfg)
			if handler != nil || err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("invalid settings accepted: handler=%v error=%v", handler, err)
			}
		})
	}
	if handler, err := FromConfig(nil, config.HTTPConfig{}); handler != nil || err == nil {
		t.Fatal("nil handler accepted")
	}
}

func TestFromConfigOrderAndIsolation(t *testing.T) {
	cfg := config.HTTPConfig{
		TrustedProxies: []string{"192.0.2.1/32"}, HSTSMaxAge: 300,
		SharedRateLimit: config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Minute, MaxClients: 10},
	}
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	})
	first, err := FromConfig(next, cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FromConfig(next, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Constructed middleware must not retain mutable configuration.
	cfg.TrustedProxies[0] = "invalid"
	cfg.HSTSMaxAge = 0
	cfg.SharedRateLimit.Requests = 100

	for _, test := range []struct {
		name, peer, client, scheme string
		handler                    http.Handler
		status                     int
		hsts                       bool
	}{
		{"invalid metadata", "192.0.2.1", "203.0.113.1", "", first, 400, false},
		{"valid after invalid", "192.0.2.1", "203.0.113.1", "https", first, 204, true},
		{"limited with HSTS", "192.0.2.1", "203.0.113.1", "https", first, 429, true},
		{"different forwarded client", "192.0.2.1", "203.0.113.2", "https", first, 204, true},
		{"independent wrapper", "192.0.2.1", "203.0.113.1", "https", second, 204, true},
		{"untrusted spoof", "192.0.2.2", "203.0.113.3", "https", first, 204, false},
		{"changed spoof cannot bypass", "192.0.2.2", "203.0.113.4", "https", first, 429, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
			r.RemoteAddr = test.peer + ":1234"
			r.Header.Set("X-Forwarded-For", test.client)
			if test.scheme != "" {
				r.Header.Set("X-Forwarded-Proto", test.scheme)
			}
			before := calls
			w := httptest.NewRecorder()
			test.handler.ServeHTTP(w, r)
			wantCalls, wantHSTS := before, ""
			if test.status == http.StatusNoContent {
				wantCalls++
			}
			if test.hsts {
				wantHSTS = "max-age=300"
			}
			if w.Code != test.status || w.Header().Get("Strict-Transport-Security") != wantHSTS || calls != wantCalls {
				t.Fatalf("status=%d HSTS=%q calls=%d", w.Code, w.Header().Get("Strict-Transport-Security"), calls)
			}
		})
	}
}

func TestFromConfigDisabledPolicies(t *testing.T) {
	handler, err := FromConfig(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), config.HTTPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "https://example.test/", nil)
		r.Header.Set("Origin", "https://other.test")
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusNoContent || len(w.Header()) != 0 {
			t.Fatal("disabled configuration added rate, HSTS, or application security policies")
		}
	}
}
