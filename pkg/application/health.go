package application

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

// HealthHandler adds GET /health/live and GET /health/ready outside session
// loading, rate limits, and module routing. Readiness checks the run context,
// configured accounts and sessions, then the supplied application checks.
// Pass the application's run context, not a startup-only context.
// Mail is optional: include MailHealth explicitly if delivery must affect readiness.
// All checks share timeout and must honor cancellation. Construct before serving.
func (a *Application) HealthHandler(ctx context.Context, timeout time.Duration, checks ...health.Check) (http.Handler, error) {
	if a == nil || a.Handler == nil {
		return nil, errors.New("health endpoints require an initialized application handler")
	}
	required := []health.Check{{Name: "application", Run: func(context.Context) error { return ctx.Err() }}}
	if a.Accounts != nil {
		accounts := a.Accounts
		required = append(required, health.Check{Name: "accounts", Run: func(ctx context.Context) error {
			// Repository IDs are provider-specific. Use a valid email for the probe.
			_, err := accounts.GetByEmail(ctx, "health-probe@example.invalid")
			if errors.Is(err, user.ErrUserNotFound) {
				return nil
			}
			return err
		}})
	}
	if a.Sessions != nil {
		required = append(required, health.Check{Name: "sessions", Run: a.Sessions.CheckHealth})
	}
	required = append(required, checks...)
	ready, err := health.Readiness(timeout, required...)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	logRequests := middleware.LoggerMiddleware(logger.Get(ctx))
	mux.Handle("GET /health/live", logRequests(http.HandlerFunc(health.Liveness)))
	mux.Handle("GET /health/ready", logRequests(ready))
	mux.Handle("/", a.Handler)
	return mux, nil
}
