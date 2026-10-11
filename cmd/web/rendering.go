package main

import (
	site "github.com/trentnix/hyperserver/modules/site"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/flash"
)

// configureRendering selects the reference site's presentation. Apply application
// overrides after this function and before initializing modules or serving requests.
func configureRendering(app *server.ApplicationServer) error {
	if err := site.ConfigureRendering(app.ContentManager, app.Config.Auth.Enabled); err != nil {
		return err
	}
	if app.Config.App.RenderNotifications {
		app.ContentManager.LoadNotifications = flash.GetNotifications
	}
	return nil
}
