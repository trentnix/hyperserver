package main

import (
	"context"

	"github.com/trentnix/hyperserver/modules/site/storage"
	"github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/flash"

	// Diagnostic and sample routes belong to this module, not the framework.
	// Production applications must not import it.
	site "github.com/trentnix/hyperserver/modules/site"
)

// siteModules selects contact storage. The framework supplies auth dependencies.
func siteModules(ctx context.Context, app *application, directory string) ([]handlers.Descriptor, error) {
	if directory == "" {
		db, err := sqlite.OpenConfigured(app.Config.Database, app.Config.App.WorkingDirectory)
		if err != nil {
			return nil, err
		}
		app.contactDB = db
		app.OnShutdown("contact storage", func(context.Context) error { return db.Close() })
		if err := db.PingContext(ctx); err != nil {
			return nil, err
		}
	}
	newContacts := func(ctx context.Context) (site.ContactRepository, error) {
		if directory != "" {
			return storage.NewFileContactRepository(ctx, directory)
		}
		contacts, err := storage.NewSQLiteContactRepository(ctx, app.contactDB)
		if err == nil {
			app.contactHealth = contacts.CheckHealth
		}
		return contacts, err
	}
	return handlers.Replace(handlers.Registered(), site.Module(newContacts, app.Mail))
}

// configureRendering selects the reference site's presentation before module setup.
func configureRendering(app *server.ApplicationServer) error {
	if err := site.ConfigureRendering(app.ContentManager, app.Config.Auth.Enabled); err != nil {
		return err
	}
	if app.Config.App.RenderNotifications {
		app.ContentManager.LoadNotifications = flash.LoadNotifications
	}
	return nil
}
