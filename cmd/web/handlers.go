package main

import (
	"context"
	"time"

	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

// SetupHandlers initializes registered modules with a bounded context and binds
// their routes. Each module prepares its own storage before its routes are bound.
func SetupHandlers(ctx context.Context, s *server.ApplicationServer) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	routes := routing.NewRoutes(s.Web)

	for _, h := range handlers.GetHandlers() {
		if err := h.Init(ctx, s); err != nil {
			return err
		}

		h.Routes(routes)
	}

	return nil
}
