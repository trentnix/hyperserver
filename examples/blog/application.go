package main

import (
	"context"
	"errors"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/examples/blog/internal/blog"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
)

// newApplication selects the blog and authentication modules with one shared
// administrator rule. The framework initializes and owns the configured services.
func newApplication(ctx context.Context, cfg config.Config) (*application.Application, error) {
	if cfg.HTTP.ListenHost == "" || cfg.HTTP.Port == 0 {
		return nil, errors.New("configure http.listenHost and a nonzero http.port")
	}
	access := &blog.Access{}
	settings := &blog.Settings{}
	return application.New(ctx, cfg, application.Options{
		ContentSecurityPolicy: blog.ContentSecurityPolicy,
		Modules: func(_ context.Context, app *application.Application) ([]handlers.Descriptor, error) {
			if err := blog.ConfigureRendering(app.ContentManager, settings, access, app.Accounts, cfg.Auth.RegistrationEnabled); err != nil {
				return nil, err
			}
			return []handlers.Descriptor{blog.Module(access, settings), auth.Module(nil, nil, nil)}, nil
		},
		AuthProviders: func(_ context.Context, app *application.Application) ([]auth.Descriptor, error) {
			return []auth.Descriptor{{Name: "email", New: func() auth.AuthService {
				return blog.NewEmailProvider(app.Accounts, app.Sessions, app.Mail, access)
			}}}, nil
		},
	})
}
