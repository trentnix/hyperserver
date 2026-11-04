package application

import (
	"context"
	"errors"
	"slices"

	"github.com/trentnix/hyperserver/auth"
	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	_ "github.com/trentnix/hyperserver/auth/modules/email"
	_ "github.com/trentnix/hyperserver/auth/modules/google"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/util"
)

// configureAuthentication supplies services without implicitly selecting modules.
// The auth module validates its manager and enabled providers during initialization.
func (a *Application) configureAuthentication(ctx context.Context, catalog []handlers.Descriptor, selectProviders func(context.Context, *Application) ([]auth.Descriptor, error), policy auth.Policy) ([]handlers.Descriptor, error) {
	a.Handler = a.Web
	if a.Sessions != nil {
		a.Handler = session.WithSessionManager(a.Sessions)(a.Handler)
	}

	if !a.Config.Auth.Enabled {
		return slices.DeleteFunc(slices.Clone(catalog), func(d handlers.Descriptor) bool { return d.Name == auth.ModuleName }), nil
	}
	selected := slices.ContainsFunc(catalog, func(d handlers.Descriptor) bool { return d.Name == auth.ModuleName })
	if !selected {
		return catalog, nil
	}

	if err := flash.ValidateSessionManager(a.Sessions); err != nil {
		return nil, err
	}
	cm := a.ContentManager
	if cm.AuthURL == "" {
		cm.AuthURL = "/login"
	}
	if util.SafeRedirectURL(cm.AuthURL, "") != cm.AuthURL {
		return nil, errors.New("auth login URL must be a local absolute path")
	}
	if cm.HomeURL == "" || util.SafeRedirectURL(cm.HomeURL, "") != cm.HomeURL {
		return nil, errors.New("auth home URL must be a local absolute path")
	}
	var providers []auth.Descriptor
	if selectProviders == nil {
		providers = auth.Registered()
	} else {
		var err error
		providers, err = selectProviders(ctx, a)
		if err != nil {
			return nil, err
		}
	}
	catalog, err := handlers.Replace(catalog, auth.Module(auth.Dependencies{Accounts: a.Accounts, Sessions: a.Sessions, Mail: a.Mail, Policy: policy}, providers))
	if err != nil {
		return nil, err
	}
	a.Handler = authmiddleware.LoadSessionManagement(a.Accounts, a.Sessions)(a.Web)
	return catalog, nil
}
