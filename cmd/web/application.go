package main

import (
	"context"
	"errors"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/blog"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
)

// newApplication selects the blog and authentication modules with one shared
// administrator rule. The framework initializes and owns the configured services.
func newApplication(ctx context.Context, cfg config.Config) (*application.Application, error) {
	if cfg.HTTP.ListenHost == "" || cfg.HTTP.Port == 0 {
		return nil, errors.New("configure http.listenHost and a nonzero http.port")
	}
	site := &blog.Site{}
	return application.New(ctx, cfg, application.Options{
		AuthPolicy:            site.AuthenticationPolicy(),
		ContentSecurityPolicy: blog.ContentSecurityPolicy,
		Modules: func(_ context.Context, app *application.Application) ([]handlers.Descriptor, error) {
			module, err := site.Configure(app)
			if err != nil {
				return nil, err
			}
			return []handlers.Descriptor{module, auth.Module(auth.Dependencies{}, nil)}, nil
		},
	})
}
