package routing

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
)

// NewConfiguredRoutes applies the configured per-route default to registrations.
// Module and handler scopes can override it. A non-nil log receives startup
// warnings when a route allowance exceeds the shared budget over the same window.
// This function does not install the shared budget. Use middleware.FromConfig on
// the request handler for that. No handlers are registered during construction.
func NewConfiguredRoutes(mux *http.ServeMux, cfg config.HTTPConfig, log *slog.Logger) (*Routes, error) {
	routes := NewRoutes(mux)
	if err := routes.Err(); err != nil {
		return nil, err
	}
	if shared := cfg.SharedRateLimit; shared.Enabled && log != nil {
		routes.OnRateLimitRegistered = func(pattern string, policy ratelimit.Policy) {
			if policy.Window == shared.Window && policy.Requests > shared.Requests {
				log.Warn("Route allowance exceeds the shared application budget per client IP. Confirm this restriction is intentional.",
					slog.Any("route", pattern),
					slog.Any("routeRequests", policy.Requests),
					slog.Any("sharedRequests", shared.Requests),
					slog.Any("window", policy.Window.String()),
				)
			}
		}
	}

	limit := cfg.DefaultRateLimit
	if !limit.Enabled {
		return routes, nil
	}
	routes, err := routes.WithRateLimit(ratelimit.Policy{Requests: limit.Requests, Window: limit.Window, MaxClients: limit.MaxClients})
	if err != nil {
		return nil, fmt.Errorf("http.defaultRateLimit: %w", err)
	}
	return routes, nil
}
