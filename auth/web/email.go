// Package web assembles optional authentication setups for web applications.
// Applications supply initialized services and retain ownership of their cleanup.
package web

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	"github.com/trentnix/hyperserver/auth"
	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	emailauth "github.com/trentnix/hyperserver/auth/modules/email"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// Email selects email/password authentication with application-owned services.
// Configure all fields before creating guards or calling Setup. Mail can be nil
// when the enabled workflows do not require delivery. LoginURL and HomeURL must
// be local absolute paths. Changing these fields does not reconfigure a running app.
type Email struct {
	Accounts user.AccountRepository
	Sessions *session.SessionManager
	Mail     *messaging.MailClient
	LoginURL string
	HomeURL  string
}

// RequireAuthentication guards a route using this setup's accounts. Apply it only
// to protected routes. Serve them through the handler returned by Setup.
func (e Email) RequireAuthentication(cm *content.ContentManagerService) func(http.Handler) http.Handler {
	return authmiddleware.RequireAuthentication(e.Accounts, cm)
}

// Setup configures email authentication, initializes the selected modules, and
// returns their HTTP handler with session and identity loading already attached.
// The catalog must contain exactly one auth descriptor, which Setup replaces in
// a copy. Use handlers.Registered() explicitly to select imported modules.
// Auth and the email provider must be enabled in app.Config.
//
// Configure layouts and rendering overrides first. Setup assigns AuthURL, HomeURL,
// and the flash notification loader. Layout data and other callbacks are unchanged.
// It uses the standard route defaults and dependency resolution without selections.
// Use the lower-level auth and handlers packages for other provider arrangements.
//
// Call once before serving. On failure, no handler is returned. Discard app and
// close application-owned services. Module rollback and shutdown follow
// handlers.Setup. This method never opens or closes supplied services.
func (e Email) Setup(ctx context.Context, app *server.ApplicationServer, catalog []handlers.Descriptor) (http.Handler, error) {
	if app == nil || app.Config == nil || app.Web == nil || app.ContentManager == nil {
		return nil, errors.New("email web setup requires an initialized application")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !app.Config.Auth.Enabled {
		return nil, errors.New("email web setup requires auth.enabled")
	}
	value := reflect.ValueOf(e.Accounts)
	if !value.IsValid() {
		return nil, errors.New("email web setup requires an account repository")
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil, errors.New("email web setup requires an account repository")
		}
	}
	if err := flash.ValidateSessionManager(e.Sessions); err != nil {
		return nil, err
	}
	if e.LoginURL == "" || util.SafeRedirectURL(e.LoginURL, "") != e.LoginURL {
		return nil, errors.New("auth login URL must be a local absolute path")
	}
	if e.HomeURL == "" || util.SafeRedirectURL(e.HomeURL, "") != e.HomeURL {
		return nil, errors.New("auth home URL must be a local absolute path")
	}
	providers := []auth.Descriptor{emailauth.Provider(e.Accounts, e.Sessions, e.Mail)}
	catalog, err := handlers.Replace(catalog, auth.Module(e.Accounts, e.Sessions, providers))
	if err != nil {
		return nil, err
	}

	app.ContentManager.AuthURL, app.ContentManager.HomeURL = e.LoginURL, e.HomeURL
	app.ContentManager.LoadNotifications = flash.LoadNotifications
	if err := handlers.Setup(ctx, app, nil, catalog, nil); err != nil {
		return nil, err
	}
	return authmiddleware.LoadSessionManagement(e.Accounts, e.Sessions)(app.Web), nil
}
