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

// SetupHandlers selects the reference application's route defaults and delegates
// module initialization, registration, and cleanup to handlers.Setup.
func SetupHandlers(ctx context.Context, s *server.ApplicationServer, log logger.Logger, catalog []handlers.Descriptor, providers map[string]string) error {
	routes, err := applicationRoutes(s.Web, s.Config.HTTP, log)
	if err != nil {
		return err
	}
	// Resolve for diagnostics, but initialize the original catalog so multiple
	// providers retain their declared binding order, not initialization order.
	resolved, err := handlers.Resolve(catalog, providers)
	if err != nil {
		return err
	}
	if log != nil {
		ctx = logger.Set(ctx, &log)
	}
	if err := handlers.Setup(ctx, s, routes, catalog, providers); err != nil {
		return err
	}

	if log != nil {
		var modules, capabilities []string
		for _, module := range resolved {
			modules = append(modules, module.Name)
			for _, capability := range module.Provides {
				capabilities = append(capabilities, capability+"="+module.Name)
			}
		}
		log.Info("Modules initialized",
			logger.Field{Key: "modules", Value: modules},
			logger.Field{Key: "capabilityProviders", Value: capabilities},
			logger.Field{Key: "providerSelections", Value: providers})
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
