package application_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/health"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type healthAccounts struct {
	user.AccountRepository
	err error
}

func (a *healthAccounts) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, a.err
}

func TestHealthEndpointsUseSelectedServicesAndRunContext(t *testing.T) {
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(logger.Set(t.Context(), slog.New(slog.NewTextHandler(&logs, nil))))
	defer cancel()
	accounts := &healthAccounts{err: user.ErrUserNotFound}
	mailCalls := 0
	app, err := application.New(ctx, config.Config{}, application.Options{Services: application.Services{
		Accounts: accounts,
		MailHealth: func(context.Context) error {
			mailCalls++
			return errors.New("mail offline")
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	var extraErr error
	handler, err := app.HealthHandler(ctx, time.Second, health.Check{Name: "contacts", Run: func(context.Context) error { return extraErr }})
	if err != nil {
		t.Fatal(err)
	}
	check := func(path string, status int) {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != status || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s: status=%d headers=%v", path, w.Code, w.Header())
		}
		if strings.Contains(w.Body.String(), "private") {
			t.Fatal("probe exposed private diagnostics")
		}
	}
	check("/health/ready", http.StatusNoContent)
	accounts.err = errors.New("private account failure")
	check("/health/ready", http.StatusServiceUnavailable)
	check("/health/live", http.StatusNoContent)
	accounts.err = nil // A stored account with the probe address also indicates availability.
	check("/health/ready", http.StatusNoContent)
	extraErr = errors.New("private contact failure")
	check("/health/ready", http.StatusServiceUnavailable)
	extraErr = nil
	cancel()
	check("/health/ready", http.StatusServiceUnavailable)
	check("/health/live", http.StatusNoContent)
	if mailCalls != 0 || strings.Contains(logs.String(), "private") {
		t.Fatal("optional mail was probed or private diagnostics were logged")
	}
}

func TestHealthEndpointsBypassAuthAndRateLimits(t *testing.T) {
	cfg := authenticationConfig(t)
	cfg.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Hour, MaxClients: 10}
	app, err := application.New(t.Context(), cfg, application.Options{Modules: authenticationModules})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	handler, err := app.HealthHandler(t.Context(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/auth/login", http.StatusBadRequest}, // Invalid cookie reaches identity loading.
		{"/auth/login", http.StatusTooManyRequests},
		{"/health/ready", http.StatusNoContent},
		{"/health/live", http.StatusNoContent},
	} {
		r := httptest.NewRequest(http.MethodGet, test.path, nil)
		r.AddCookie(&http.Cookie{Name: "auth-user-session", Value: "invalid"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s: status=%d, want %d", test.path, w.Code, test.status)
		}
	}
	if err := app.Sessions.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("closed session storage remained ready")
	}
}

func TestHealthHandlerValidationAndTimeout(t *testing.T) {
	app, err := application.New(t.Context(), config.Config{}, application.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	for _, checks := range [][]health.Check{
		{{Name: "application", Run: func(context.Context) error { return nil }}},
		{{Name: "missing function"}},
	} {
		if h, err := app.HealthHandler(t.Context(), time.Second, checks...); err == nil || h != nil {
			t.Fatal("invalid checks accepted")
		}
	}
	if h, err := app.HealthHandler(t.Context(), 0); err == nil || h != nil {
		t.Fatal("invalid timeout accepted")
	}
	for _, uninitialized := range []*application.Application{nil, {}} {
		if h, err := uninitialized.HealthHandler(t.Context(), time.Second); err == nil || h != nil {
			t.Fatal("missing handler accepted")
		}
	}
	h, err := app.HealthHandler(t.Context(), time.Millisecond, health.Check{Name: "slow", Run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("timed-out check reported readiness")
	}
}
