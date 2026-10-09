package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// SetupHandlers resolves capability requirements before creating modules, then
// initializes them in dependency order with a bounded context and binds routes.
// app.initializationTimeout sets the module budget. An earlier ctx deadline wins.
// providers selects module names by capability. Nil requires unambiguous defaults.
func SetupHandlers(ctx context.Context, s *server.ApplicationServer, log logger.Logger, catalog []handlers.Descriptor, providers map[string]string) error {
	if timeout := s.Config.App.InitializationTimeout; timeout != nil {
		if *timeout <= 0 {
			return fmt.Errorf("app.initializationTimeout must be positive")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	routes, err := applicationRoutes(s.Web, s.Config.HTTP, log)
	if err != nil {
		return err
	}

	modules, err := handlers.Initialize(ctx, s, catalog, providers)
	if err != nil {
		return err
	}
	for _, h := range modules {
		if err := h.Routes(routes); err != nil {
			return fmt.Errorf("register module %q (%T) routes: %w", h.Name, h.Handler, err)
		}
		if err := routes.Err(); err != nil {
			return fmt.Errorf("register module %q routes: %w", h.Name, err)
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
