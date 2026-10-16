package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplicationHealth(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		appHandler, err := healthHandler(ctx, h.app, h.handler)
		if err != nil {
			t.Fatal(err)
		}
		check := func(path string, want int) {
			t.Helper()
			w := httptest.NewRecorder()
			appHandler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != want {
				t.Fatalf("%s: %d", path, w.Code)
			}
		}
		check("/health/ready", 204)
		check("/health/live", 204)
		if err := h.app.Database.Close(); err != nil {
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
		h.app.mailHealth = func(context.Context) error { calls++; return errors.New("offline") }
		h.app.Config.Auth.RegisterRequiresVerification = false
		for _, required := range []bool{false, true} {
			h.app.Config.Auth.RegisterRequiresVerification = required
			h.app.Config.Auth.RegistrationEnabled = true
			handler, err := healthHandler(t.Context(), h.app, http.NotFoundHandler())
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/health/ready", nil))
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
