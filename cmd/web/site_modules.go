package main

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/trentnix/hyperserver/auth"
	email "github.com/trentnix/hyperserver/auth/modules/email"
	"github.com/trentnix/hyperserver/pkg/services/messaging"

	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/handlers"

	// Diagnostic and sample routes belong to this module, not the framework.
	// Production applications must not import it.
	site "github.com/trentnix/hyperserver/modules/site"
)

// siteModules snapshots imports and supplies the reference application's storage
// and mail dependencies without changing the process-wide catalogs.
func siteModules(db *sqlx.DB, mail *messaging.MailClient, directory string) ([]handlers.Descriptor, error) {
	catalog := handlers.Registered()
	newContacts := func(ctx context.Context) (site.ContactRepository, error) {
		if directory == "" {
			return models.NewContactRepository(ctx, db)
		}
		return models.NewFileContactRepository(ctx, directory)
	}
	catalog, err := handlers.Replace(catalog, site.Module(newContacts, mail))
	if err != nil {
		return nil, err
	}

	providers := auth.Registered()
	for i, provider := range providers {
		if provider.Name == email.AuthTypeEmail {
			providers[i] = email.Provider(mail)
		}
	}
	return handlers.Replace(catalog, auth.Module(providers))
}
