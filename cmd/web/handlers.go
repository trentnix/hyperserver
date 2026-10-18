package main

import (
	"context"
	"log/slog"

	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// SetupHandlers selects the reference application's route defaults and delegates
// module initialization, registration, and cleanup to handlers.Setup.
func SetupHandlers(ctx context.Context, s *server.ApplicationServer, log *slog.Logger, catalog []handlers.Descriptor, providers map[string]string) error {
	routes, err := routing.NewConfiguredRoutes(s.Web, s.Config.HTTP, log)
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
		ctx = logger.Set(ctx, log)
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
		log.InfoContext(ctx, "Modules initialized",
			slog.Any("modules", modules),
			slog.Any("capabilityProviders", capabilities),
			slog.Any("providerSelections", providers))
	}
	return nil
}
