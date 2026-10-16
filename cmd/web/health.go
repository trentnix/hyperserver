package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/trentnix/hyperserver/pkg/health"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// healthHandler sits outside session loading, rate limits, and module routing.
// The listener starts only after initialization and closes before resources do.
func healthHandler(ctx context.Context, app *application, next http.Handler) (http.Handler, error) {
	checks := []health.Check{{Name: "application", Run: func(context.Context) error { return ctx.Err() }}}
	if app.Database != nil {
		checks = append(checks, health.Check{Name: "application storage", Run: app.Database.PingContext})
	}
	if app.contactHealth != nil {
		checks = append(checks, health.Check{Name: "contacts", Run: app.contactHealth})
	}
	if app.Config.Auth.Enabled {
		checks = append(checks, health.Check{Name: "accounts", Run: func(ctx context.Context) error {
			if app.AccountRepository == nil {
				return errors.New("account repository is unavailable")
			}
			// Email syntax is part of the repository contract. Account-ID formats
			// belong to providers, so a made-up ID would not be portable.
			_, err := app.AccountRepository.GetByEmail(ctx, "health-probe@example.invalid")
			if errors.Is(err, user.ErrUserNotFound) {
				return nil
			}
			return err
		}})
	}
	if app.SessionManager != nil {
		checks = append(checks, health.Check{Name: "sessions", Run: app.SessionManager.CheckHealth})
	}
	if app.mailHealth != nil && app.Config.Auth.Enabled && app.Config.Auth.RegistrationEnabled && app.Config.Auth.RegisterRequiresVerification {
		checks = append(checks, health.Check{Name: "mail", Run: app.mailHealth})
	}
	ready, err := health.Readiness(2*time.Second, checks...)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	logRequests := middleware.LoggerMiddleware(logger.Get(ctx))
	mux.Handle("GET /health/live", logRequests(http.HandlerFunc(health.Liveness)))
	mux.Handle("GET /health/ready", logRequests(ready))
	mux.Handle("/", next)
	return mux, nil
}
