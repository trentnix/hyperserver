package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// SetupHandlers initializes registered modules with a bounded context and binds
// their routes. Each module prepares its own storage before its routes are bound.
func SetupHandlers(ctx context.Context, s *server.ApplicationServer, log logger.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	routes, err := applicationRoutes(s.Web, s.Config.HTTP, log)
	if err != nil {
		return err
	}

	for _, h := range handlers.GetHandlers() {
		if err := h.Init(ctx, s); err != nil {
			return err
		}

		if err := h.Routes(routes); err != nil {
			return fmt.Errorf("register %T routes: %w", h, err)
		}
	}

	return nil
}

// applicationRoutes selects defaults without allocating shared request counters.
func applicationRoutes(mux *http.ServeMux, cfg config.HTTPConfig, log logger.Logger) (*routing.Routes, error) {
	routes := routing.NewRoutes(mux)
	if shared := cfg.SharedRateLimit; shared.Enabled && log != nil {
		routes.OnRateLimitRegistered = func(pattern string, policy ratelimit.Policy) {
			if policy.Window == shared.Window && policy.Requests > shared.Requests {
				log.Warn("Route allowance exceeds the shared application budget per client IP. Confirm this restriction is intentional.",
					logger.Field{Key: "route", Value: pattern},
					logger.Field{Key: "routeRequests", Value: policy.Requests},
					logger.Field{Key: "sharedRequests", Value: shared.Requests},
					logger.Field{Key: "window", Value: policy.Window.String()},
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
