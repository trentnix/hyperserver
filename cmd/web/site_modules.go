package main

import (
	"context"

	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/handlers"

	// Diagnostic and sample routes belong to this module, not the framework.
	// Production applications must not import it.
	site "github.com/trentnix/hyperserver/modules/site"
)

// configureContactStorage selects storage for the site without changing account
// storage. This is reference-application wiring, not a framework provider registry.
func configureContactStorage(directory string) {
	for _, handler := range handlers.GetHandlers() {
		if module, ok := handler.(*site.SiteModule); ok {
			module.NewContacts = nil
			if directory != "" {
				module.NewContacts = func(ctx context.Context) (site.ContactRepository, error) {
					return models.NewFileContactRepository(ctx, directory)
				}
			}
		}
	}
}
