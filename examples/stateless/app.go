package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/examples/stateless/pages"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

// newApplication composes only the modules and services this application needs.
// No configuration file, storage, sessions, authentication, or mail is required.
func newApplication(ctx context.Context, name string) (_ *server.ApplicationServer, err error) {
	app, err := server.NewApplicationServer(config.Config{App: config.AppConfig{Name: name}})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, app.Shutdown())
		}
	}()

	// Imports make modules discoverable. This application explicitly selects its
	// own module so unrelated imports in the same process cannot add routes.
	catalog := []handlers.Descriptor{pages.Module()}
	modules, err := handlers.Initialize(ctx, app, catalog, nil)
	if err != nil {
		return nil, err
	}
	app.OnShutdown("modules", func(ctx context.Context) error { return handlers.Close(ctx, modules) })

	routes := routing.NewRoutes(app.Web)
	for _, module := range modules {
		if err := module.Routes(routes); err != nil {
			return nil, fmt.Errorf("register %s routes: %w", module.Name, err)
		}
		if err := routes.Err(); err != nil {
			return nil, err
		}
	}
	return app, nil
}
