package blog

import (
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/application"
	"github.com/trentnix/hyperserver/pkg/handlers"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

// Site assembles one application's blog module, rendering, and administrator-only
// authentication policy. Its zero value is ready to configure. Create one Site per
// application and do not copy it. Assembly methods run during startup, before
// requests are accepted. The application still selects services and modules.
type Site struct {
	access     Access
	profile    ProfileState
	configured bool
	content    *contentservice.ContentManagerService

	// Module initialization supplies navigation after its storage opens.
	populateNavigation func(*http.Request, navigation) navigation
}

// Configure sets up shared rendering and returns the blog descriptor. Call it
// from application.Options.Modules, before views are prepared. A Site can be
// configured only once. The module owns its content and image storage, while
// application services retain their existing owner.
func (s *Site) Configure(app *application.Application) (handlers.Descriptor, error) {
	if s.configured {
		return handlers.Descriptor{}, errors.New("blog site is already configured")
	}
	if app.Accounts == nil || !app.Config.Auth.Enabled {
		return handlers.Descriptor{}, errors.New("blog requires authentication and account storage")
	}
	if err := s.configureRendering(app.ContentManager, app.Accounts, app.Config.Auth.RegistrationEnabled); err != nil {
		return handlers.Descriptor{}, err
	}
	s.configured = true
	s.content = app.ContentManager
	accounts := app.Accounts
	return handlers.Descriptor{Name: "blog", Requires: []handlers.Requirement{{Capability: auth.ManagerCapability}}, New: func() handlers.Handler {
		return &module{settings: &Settings{}, access: &s.access, profile: &s.profile, accounts: accounts, site: s}
	}}, nil
}
