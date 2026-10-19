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
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestDefaultWebMiddlewareWithoutServices(t *testing.T) {
	app, err := application.New(t.Context(), config.Config{}, application.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	app.Web.HandleFunc("POST /submit", func(w http.ResponseWriter, r *http.Request) {
		if logger.Get(r.Context()) != nil || session.GetSessionManager(r) != nil {
			t.Error("unconfigured logger or sessions appeared")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, test := range []struct {
		method, path, origin string
		status               int
	}{
		{http.MethodPost, "/submit", "http://example.test", http.StatusNoContent},
		{http.MethodPost, "/submit", "https://other.test", http.StatusForbidden},
		{http.MethodGet, "/missing", "", http.StatusNotFound},
	} {
		r := httptest.NewRequest(test.method, "http://example.test"+test.path, nil)
		r.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		app.Handler.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s %s: status=%d, want %d", test.method, test.path, w.Code, test.status)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("X-Request-ID") == "" {
			t.Fatal("default headers missing")
		}
		if w.Header().Get("Content-Security-Policy") != "" || w.Header().Get("Strict-Transport-Security") != "" {
			t.Fatal("unconfigured CSP or HSTS was added")
		}
	}
}

func TestConfiguredWebMiddlewareWrapsAuthentication(t *testing.T) {
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(logger.Set(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil))))
	defer cancel()
	cfg := authenticationConfig(t)
	cfg.HTTP.PublicOrigin = "https://example.test"
	cfg.HTTP.TrustedProxies = []string{"192.0.2.1/32"}
	cfg.HTTP.HSTSMaxAge = 300
	cfg.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true, Requests: 3, Window: time.Hour, MaxClients: 10}
	calls := 0
	app, err := application.New(ctx, cfg, application.Options{
		ContentSecurityPolicy: "default-src 'none'",
		Modules:               authenticationModules,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown() })
	cancel() // Request contexts must not inherit the startup deadline or cancellation.
	app.Web.HandleFunc("POST /submit", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !requestinfo.IsHTTPS(r) || logger.Get(r.Context()) == nil || session.GetSessionManager(r) != app.Sessions || r.Context().Err() != nil {
			t.Error("request did not receive independent context, verified HTTPS, logger, and sessions")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, test := range []struct {
		name, scheme, origin string
		badCookie            bool
		status               int
	}{
		{"invalid proxy", "", "", true, http.StatusBadRequest},
		{"cross-origin", "https", "https://other.test", true, http.StatusForbidden},
		{"same-origin", "https", "https://example.test", false, http.StatusNoContent},
		{"invalid identity", "https", "https://example.test", true, http.StatusBadRequest},
		{"limited", "https", "https://example.test", true, http.StatusTooManyRequests},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://example.test/submit", nil)
			r.RemoteAddr = "192.0.2.1:1234"
			r.Header.Set("X-Forwarded-For", "203.0.113.1")
			r.Header.Set("X-Forwarded-Proto", test.scheme)
			r.Header.Set("Origin", test.origin)
			if test.badCookie {
				r.AddCookie(&http.Cookie{Name: "auth-user-session", Value: "invalid"})
			}
			w := httptest.NewRecorder()
			app.Handler.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status=%d, want %d: %s", w.Code, test.status, w.Body)
			}
			if w.Header().Get("Content-Security-Policy") != "default-src 'none'" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("X-Request-ID") == "" {
				t.Fatal("outer headers missing")
			}
			if test.scheme != "" && w.Header().Get("Strict-Transport-Security") != "max-age=300" {
				t.Fatal("verified HTTPS response lost HSTS")
			}
		})
	}
	if calls != 1 || strings.Count(logs.String(), "Request completed") != 5 {
		t.Fatalf("handler calls=%d, logs=%s", calls, &logs)
	}
}

func TestApplicationRateLimitsAreAppliedOnceAndIsolated(t *testing.T) {
	cfg := config.Config{HTTP: config.HTTPConfig{
		DefaultRateLimit: config.RateLimitConfig{Enabled: true, Requests: 1, Window: time.Hour, MaxClients: 10},
		SharedRateLimit:  config.RateLimitConfig{Enabled: true, Requests: 3, Window: time.Hour, MaxClients: 10},
	}}
	options := application.Options{Modules: func(context.Context, *application.Application) ([]handlers.Descriptor, error) {
		return []handlers.Descriptor{{Name: "feature", New: func() handlers.Handler {
			return &moduleStub{routes: func(routes *routing.Routes) error {
				for _, path := range []string{"/one", "/two", "/three"} {
					routes.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
				}
				return routes.Err()
			}}
		}}}, nil
	}}
	var apps []*application.Application
	for range 2 {
		app, err := application.New(t.Context(), cfg, options)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { app.Shutdown() })
		apps = append(apps, app)
	}
	for _, app := range apps {
		for _, test := range []struct {
			path   string
			status int
		}{
			{"/one", http.StatusNoContent},
			{"/one", http.StatusTooManyRequests},
			{"/two", http.StatusNoContent},
			{"/three", http.StatusTooManyRequests},
		} {
			w := browserRequests(t, app, false)(http.MethodGet, test.path, nil)
			if w.Code != test.status {
				t.Fatalf("%s: status=%d, want %d", test.path, w.Code, test.status)
			}
		}
	}
}

func TestMiddlewareFailureClosesModulesBeforeOwnedServices(t *testing.T) {
	for _, setting := range []string{"trustedProxies", "hstsMaxAge", "sharedRateLimit"} {
		t.Run(setting, func(t *testing.T) {
			var captured *application.Application
			closed := false
			closeErr := errors.New("module cleanup failed")
			app, err := application.New(t.Context(), authenticationConfig(t), application.Options{
				Modules: func(_ context.Context, app *application.Application) ([]handlers.Descriptor, error) {
					captured = app
					// Exercise final middleware validation after services have opened.
					switch setting {
					case "trustedProxies":
						app.Config.HTTP.TrustedProxies = []string{"invalid"}
					case "hstsMaxAge":
						app.Config.HTTP.HSTSMaxAge = -1
					case "sharedRateLimit":
						app.Config.HTTP.SharedRateLimit = config.RateLimitConfig{Enabled: true}
					}
					return []handlers.Descriptor{{Name: "feature", New: func() handlers.Handler {
						return &moduleStub{shutdown: func(ctx context.Context) error {
							closed = true
							if err := app.Sessions.CheckHealth(ctx); err != nil {
								t.Error("services closed before modules", err)
							}
							return closeErr
						}}
					}}}, nil
				},
			})
			if app != nil || err == nil || !strings.Contains(err.Error(), "prepare HTTP middleware: http."+setting) || !errors.Is(err, closeErr) || !closed {
				t.Fatalf("app=%v err=%v module closed=%t", app, err, closed)
			}
			if err := captured.Sessions.CheckHealth(t.Context()); err == nil {
				t.Fatal("sessions remained open")
			}
			if _, err := captured.Accounts.GetByEmail(t.Context(), "reader@example.com"); err == nil || !strings.Contains(err.Error(), "database is closed") {
				t.Fatal("accounts remained open", err)
			}
		})
	}
}
