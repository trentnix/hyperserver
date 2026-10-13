package main

import (
	"context"
	"database/sql"

	"github.com/trentnix/hyperserver/auth"
	email "github.com/trentnix/hyperserver/auth/modules/email"

	"github.com/trentnix/hyperserver/modules/site/storage"
	"github.com/trentnix/hyperserver/pkg/handlers"

	// Diagnostic and sample routes belong to this module, not the framework.
	// Production applications must not import it.
	site "github.com/trentnix/hyperserver/modules/site"
)

// siteModules deliberately selects the registered catalogs and supplies the
// application's accounts, sessions, contact storage, and mail to their consumers.
// Configuring these factories does not change the process-wide catalogs.
func siteModules(app *application, directory string) ([]handlers.Descriptor, error) {
	catalog := handlers.Registered()
	var db *sql.DB
	if app.Database != nil {
		db = app.Database.DB
	}
	newContacts := func(ctx context.Context) (site.ContactRepository, error) {
		if directory == "" {
			return storage.NewSQLiteContactRepository(ctx, db)
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
