// Package authpages supplies site-owned routes for authentication integration tests.
package authpages

import (
	"context"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

func Module() handlers.Descriptor {
	return handlers.Descriptor{Name: "auth-pages", Requires: []handlers.Requirement{{Capability: auth.ManagerCapability, Optional: true}}, New: func() handlers.Handler { return &pages{} }}
}

type pages struct{ manager *auth.AuthManager }

func (p *pages) BindDependencies(d handlers.Dependencies) error {
	var err error
	p.manager, err = handlers.GetDependency[*auth.AuthManager](d, auth.ManagerCapability)
	return err
}

func (*pages) Init(context.Context, *server.ApplicationServer) error { return nil }

func (p *pages) Routes(routes *routing.Routes) error {
	if p.manager == nil || !p.manager.Enabled {
		return nil
	}
	handlers, err := p.manager.PageHandlers(routes)
	if err != nil {
		return err
	}
	routes.Handle("GET /login", handlers.Login)
	handlers.Submissions.Handle("POST /login", handlers.LoginSubmit)
	routes.Handle("GET /register", handlers.Registration)
	handlers.Submissions.Handle("POST /register", handlers.RegistrationSubmit)
	return routes.Err()
}
