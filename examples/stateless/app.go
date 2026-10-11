package main

import (
	"context"
	"errors"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/examples/stateless/pages"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
)

// newApplication composes only the modules and services this application needs.
// No configuration file, storage, sessions, authentication, or mail is required.
func newApplication(ctx context.Context, name string) (*server.ApplicationServer, error) {
	app, err := server.NewApplicationServer(config.Config{App: config.AppConfig{Name: name}})
	if err != nil {
		return nil, err
	}

	// Imports make modules discoverable. This application explicitly selects its
	// own module so unrelated imports in the same process cannot add routes.
	catalog := []handlers.Descriptor{pages.Module()}
	if err := pages.ConfigureRendering(app.ContentManager); err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	if err := handlers.Setup(ctx, app, nil, catalog, nil); err != nil {
		return nil, errors.Join(err, app.Shutdown())
	}
	return app, nil
}
