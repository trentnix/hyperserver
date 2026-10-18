package main

import (
	"context"
	"log/slog"

	"github.com/trentnix/hyperserver/auth"
	email "github.com/trentnix/hyperserver/auth/modules/email"

	"github.com/trentnix/hyperserver/modules/site/storage"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"

	// Diagnostic and sample routes belong to this module, not the framework.
	// Production applications must not import it.
	site "github.com/trentnix/hyperserver/modules/site"
)

// siteModules deliberately selects the registered catalogs and supplies the
// application's accounts, sessions, contact storage, and mail to their consumers.
// Configuring these factories does not change the process-wide catalogs.
func siteModules(app *application, directory string) ([]handlers.Descriptor, error) {
	catalog := handlers.Registered()
	db := app.Database
	newContacts := func(ctx context.Context) (site.ContactRepository, error) {
		if directory == "" {
			contacts, err := storage.NewSQLiteContactRepository(ctx, db)
			if err == nil {
				app.contactHealth = contacts.CheckHealth
			}
			return contacts, err
		}
		return storage.NewFileContactRepository(ctx, directory)
	}
	catalog, err := handlers.Replace(catalog, site.Module(newContacts, app.Mail))
	if err != nil {
		return nil, err
	}

	providers := auth.Registered()
	for i, provider := range providers {
		if provider.Name == email.AuthTypeEmail {
			providers[i] = email.Provider(app.AccountRepository, app.SessionManager, app.Mail)
		}
	}
	return handlers.Replace(catalog, auth.Module(app.AccountRepository, app.SessionManager, providers))
}

// configureRendering selects the reference site's presentation. Apply application
// overrides after this function and before initializing modules or serving requests.
func configureRendering(app *server.ApplicationServer) error {
	if err := site.ConfigureRendering(app.ContentManager, app.Config.Auth.Enabled); err != nil {
		return err
	}
	if app.Config.App.RenderNotifications {
		app.ContentManager.LoadNotifications = flash.LoadNotifications
	}
	return nil
}

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
