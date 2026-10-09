package main

import (
	"context"

	// Register the reference application's email authentication provider.
	_ "github.com/trentnix/hyperserver/auth/modules/email"

	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/handlers"

	// Diagnostic and sample routes belong to this module, not the framework.
	// Production applications must not import it.
	site "github.com/trentnix/hyperserver/modules/site"
)

// siteModules snapshots imports and configures the reference site's factory.
// The directory belongs to this application, not the process-wide catalog.
func siteModules(directory string) ([]handlers.Descriptor, error) {
	catalog := handlers.Registered()
	if directory == "" {
		return catalog, nil
	}
	newContacts := func(ctx context.Context) (site.ContactRepository, error) {
		return models.NewFileContactRepository(ctx, directory)
	}
	return handlers.Replace(catalog, site.Module(newContacts))
}
