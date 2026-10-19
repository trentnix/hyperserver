// Package application assembles configured services and modules around the core server.
// Use server directly for applications that do not need the built-in providers.
package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/configured"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/messaging/smtp"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Services contains optional services shared by application modules.
// Supplied services must be initialized and keep their existing owner.
type Services struct {
	Accounts user.AccountRepository
	Sessions *session.SessionManager
	Mail     *messaging.MailClient
}

// Options selects modules, the browser security policy, and service overrides.
// Supplied services bypass their factories. Additional factories replace matching
// built-in names and are local to this application.
type Options struct {
	// ContentSecurityPolicy must match the application's assets and HTMX settings.
	// Empty omits the Content-Security-Policy header.
	ContentSecurityPolicy string
	Services              Services
	AccountProviders      map[string]user.StorageProvider
	SessionProviders      map[string]session.ConfiguredProvider
	// AuthProviders supplies the selected auth module's provider catalog. Nil
	// selects email/password with this application's services. An empty non-nil
	// slice selects none. Configuration determines which providers are enabled.
	AuthProviders []auth.Descriptor
	// Modules selects a catalog after shared services are ready. Supply feature
	// dependencies through descriptor factories. A nil callback loads no modules.
	// Use handlers.Registered explicitly to select the import catalog. Register
	// callback-owned resources with app.OnShutdown before returning.
	Modules func(context.Context, *Application) ([]handlers.Descriptor, error)
	// ProviderSelections resolves ambiguous module capabilities by provider name.
	ProviderSelections map[string]string
}

// Application owns initialized modules and the services it creates, but not
// services supplied in Options. Run drains requests before closing modules and
// then services. Without Run, call Shutdown after consumers stop.
type Application struct {
	*server.ApplicationServer
	Services
	// Handler wraps Web with standard web middleware, session loading, and,
	// when auth is selected and enabled, identity loading. Serve this handler,
	// not the bare Web mux. Modules choose route authorization.
	Handler http.Handler
}

// New copies and validates configuration, resolves paths, and opens selected
// services, then initializes selected modules and binds their routes. It loads
// optional module configuration, but not the application configuration file.
// It installs standard web middleware around session and identity loading,
// using the logger from ctx. It does not open a listener.
// Consumers must validate required services and choose protected routes.
// All startup shares app.initializationTimeout, or ten seconds if unset. An earlier
// ctx deadline wins. Failure closes acquired resources with a fresh cleanup
// deadline. Startup contexts must not be retained for request handling.
func New(ctx context.Context, cfg config.Config, options Options) (_ *Application, err error) {
	core, err := server.NewApplicationServer(cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, core.ShutdownContext(context.WithoutCancel(ctx)))
		}
	}()

	timeout := 10 * time.Second
	if core.Config.App.InitializationTimeout != nil {
		timeout = *core.Config.App.InitializationTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := core.Config.ResolvePaths(); err != nil {
		return nil, err
	}

	app := &Application{ApplicationServer: core, Services: options.Services}
	if err := app.openServices(ctx, options); err != nil {
		return nil, err
	}

	var catalog []handlers.Descriptor
	if options.Modules != nil {
		catalog, err = options.Modules(ctx, app)
		if err != nil {
			return nil, fmt.Errorf("select modules: %w", err)
		}
	}
	if app.Config.App.RenderNotifications && app.ContentManager.LoadNotifications == nil {
		if err := flash.ValidateSessionManager(app.Sessions); err != nil {
			return nil, fmt.Errorf("prepare notifications: %w", err)
		}
		app.ContentManager.LoadNotifications = flash.LoadNotifications
	}

	catalog, err = app.configureAuthentication(catalog, options.AuthProviders)
	if err != nil {
		return nil, fmt.Errorf("prepare authentication: %w", err)
	}
	if err := handlers.Setup(ctx, core, nil, catalog, options.ProviderSelections); err != nil {
		return nil, fmt.Errorf("prepare modules: %w", err)
	}

	app.Handler, err = middleware.Web(app.Handler, app.Config.HTTP, options.ContentSecurityPolicy, logger.Get(ctx))
	if err != nil {
		return nil, fmt.Errorf("prepare HTTP middleware: %w", err)
	}
	return app, nil
}

func (a *Application) openServices(ctx context.Context, options Options) error {
	if a.Accounts != nil {
		value := reflect.ValueOf(a.Accounts)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				return errors.New("supplied account repository is nil")
			}
		}
	} else {
		repository, cleanup, err := configured.OpenAccounts(ctx, a.Config, options.AccountProviders)
		if err != nil {
			return fmt.Errorf("prepare accounts: %w", err)
		}
		a.Accounts = repository
		if cleanup != nil {
			a.OnShutdown("accounts", func(context.Context) error { return cleanup() })
		}
	}

	if a.Sessions == nil {
		manager, err := configured.NewSessionManager(ctx, a.Config, options.SessionProviders)
		if err != nil {
			return fmt.Errorf("prepare sessions: %w", err)
		}
		if len(manager.Types) > 0 {
			a.Sessions = manager
			a.OnShutdown("sessions", func(context.Context) error { return manager.Close() })
		}
	}

	if a.Mail == nil && a.Config.Mail != (config.MailConfig{}) {
		var err error
		a.Mail, err = smtp.NewMailClient(a.Config.Mail)
		if err != nil {
			return fmt.Errorf("prepare mail: %w", err)
		}
	}
	return ctx.Err()
}
