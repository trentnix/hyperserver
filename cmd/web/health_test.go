package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestApplicationHealth(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		h.app.Config.HTTP.Port = 0
		httpServer, listener, err := prepareHTTP(ctx, h.app)
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		check := func(path string, want int) {
			t.Helper()
			w := httptest.NewRecorder()
			httpServer.Handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != want {
				t.Fatalf("%s: %d", path, w.Code)
			}
		}
		check("/health/ready", 204)
		check("/health/live", 204)
		if err := h.app.contactDB.Close(); err != nil {
			t.Fatal(err)
		}
		check("/health/ready", 503)
		check("/health/live", 204)
		cancel()
		check("/health/ready", 503)
	})
}

func TestReadinessOptionalMail(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		calls := 0
		h.app.Config.HTTP.Port = 0
		h.app.MailHealth = func(context.Context) error { calls++; return errors.New("offline") }
		h.app.Config.Auth.RegisterRequiresVerification = false
		for _, required := range []bool{false, true} {
			h.app.Config.Auth.RegisterRequiresVerification = required
			h.app.Config.Auth.RegistrationEnabled = true
			httpServer, listener, err := prepareHTTP(t.Context(), h.app)
			if err != nil {
				t.Fatal(err)
			}
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			httpServer.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/health/ready", nil))
			want := 204
			if required {
				want = 503
			}
			if w.Code != want {
				t.Fatal(w.Code)
			}
		}
		if calls != 1 {
			t.Fatal(calls)
		}
	})
}
