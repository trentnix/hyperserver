package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// Setup prepares shared response templates, loads optional module configuration,
// initializes modules, binds routes, and registers cleanup with app. Shared services and
// application rendering overrides must be ready before calling Setup. A nil
// routes value uses app.Web with http.defaultRateLimit. A nil catalog loads
// no modules. Use Registered explicitly to select the import catalog.
//
// app.initializationTimeout bounds initialization. An earlier ctx deadline wins.
// On failure, Setup closes initialized modules with a fresh cleanup deadline.
// Routes already registered remain in the mux: discard the application on failure.
// On success, application shutdown closes modules in reverse dependency order.
// Call only during startup, before serving requests.
func Setup(ctx context.Context, app *server.ApplicationServer, routes *routing.Routes, catalog []Descriptor, selections map[string]string) (err error) {
	if app == nil || app.Config == nil || app.Web == nil {
		return fmt.Errorf("module setup requires an initialized application")
	}
	if timeout := app.Config.App.InitializationTimeout; timeout != nil {
		if *timeout <= 0 {
			return fmt.Errorf("app.initializationTimeout must be positive")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	if routes == nil {
		routes, err = routing.NewConfiguredRoutes(app.Web, app.Config.HTTP, logger.Get(ctx))
		if err != nil {
			return err
		}
	}
	if err := routes.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cm := app.ContentManager; cm != nil && cm.PrepareTemplates != nil {
		if err := cm.PrepareTemplates(); err != nil {
			return fmt.Errorf("prepare shared response templates: %w", err)
		}
	}

	modules, err := Initialize(ctx, app, catalog, selections)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, Close(context.WithoutCancel(ctx), modules))
		}
	}()

	for _, module := range modules {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := module.Routes(routes); err != nil {
			return fmt.Errorf("register module %q (%T) routes: %w", module.Name, module.Handler, err)
		}
		if err := routes.Err(); err != nil {
			return fmt.Errorf("register module %q routes: %w", module.Name, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	app.OnShutdown("modules", func(ctx context.Context) error { return Close(ctx, modules) })
	if log := logger.Get(ctx); log != nil {
		var names, capabilities []string
		for _, module := range modules {
			names = append(names, module.Name)
		}
		for _, descriptor := range catalog {
			for _, capability := range descriptor.Provides {
				capabilities = append(capabilities, capability+"="+descriptor.Name)
			}
		}
		log.InfoContext(ctx, "Modules initialized", "modules", names,
			"capabilityProviders", capabilities, "providerSelections", selections)
	}
	return nil
}
