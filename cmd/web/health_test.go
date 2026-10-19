package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

type healthMailSender struct {
	messaging.MailSender // Readiness must not send a message.
	check                func(context.Context) error
}

func (s healthMailSender) CheckHealth(ctx context.Context) error { return s.check(ctx) }

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
		mail, err := messaging.NewMailClientWithSender("sender@example.com", healthMailSender{
			check: func(context.Context) error { calls++; return errors.New("offline") },
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			name     string
			required bool
			mail     *messaging.MailClient
			status   int
		}{
			{"optional", false, mail, 204},
			{"required", true, mail, 503},
			{"required but missing", true, nil, 503},
		} {
			h.app.Mail = test.mail
			h.app.Config.Auth.RegisterRequiresVerification = test.required
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
			if w.Code != test.status {
				t.Fatalf("%s: status=%d, want %d", test.name, w.Code, test.status)
			}
		}
		if calls != 1 {
			t.Fatal(calls)
		}
	})
}
