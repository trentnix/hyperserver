package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

type healthMail struct{}

func (healthMail) Send(context.Context, messaging.MailMessage) error { return nil }
func (healthMail) CheckHealth(context.Context) error                 { return errors.New("private SMTP failure") }

func TestCommandHealthDuringSetupAndShutdown(t *testing.T) {
	cfg := blogConfig(t)
	app, err := newApplication(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	app.Mail, err = messaging.NewMailClientWithSender("test@example.invalid", healthMail{})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []bool{false, true} {
		app.Config.Auth.RegisterRequiresVerification = required
		ctx, cancel := context.WithCancel(t.Context())
		s, err := newHTTPServer(ctx, app)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		check := func(path string, status int) {
			t.Helper()
			r := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			s.Handler.ServeHTTP(w, r)
			if w.Code != status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s: %d %v", path, w.Code, w.Header())
			}
			if strings.Contains(w.Body.String(), "private SMTP failure") {
				t.Fatal("probe exposed private error")
			}
		}
		check("/health/live", http.StatusNoContent)
		status := http.StatusNoContent
		if required {
			status = http.StatusServiceUnavailable
		}
		check("/health/ready", status)
		cancel()
		check("/health/ready", http.StatusServiceUnavailable)
		check("/health/live", http.StatusNoContent)
	}
}
