// site.go defines the site module
package module_site

import (
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	// SiteModule contains all of the data required to implement the site module
	SiteModule struct {
		AppName  string
		Database *sqlx.DB

		sessionManager *session.SessionManager
	}
)

const (
	module                          = "module_site"
	pageLayoutTemplate              = "modules/site/templates/html/layouts/site.html"
	messageComponentTemplate        = "modules/site/templates/html/components/content-messages.html"
	consoleMessageComponentTemplate = "modules/site/templates/html/components/console-messages.html"
	homeContent                     = "modules/site/templates/html/index.html"
	loginContent                    = "modules/site/templates/html/login.html"
	errorContent                    = "modules/site/templates/html/error.html"

	homeURL  = "/"
	authURL  = "/login"
	errorURL = "/error"
)

// init registers an instance of SiteModule with the application handlers. init runs
// when the module_site
func init() {
	handlers.Register(new(SiteModule))
}

// Init takes care of initializing the specified SiteModule instance
func (m *SiteModule) Init(s *server.ApplicationServer) error {
	m.AppName = s.Config.App.Name

	m.Database = s.Database
	m.sessionManager = session.GetSessionManager()

	contentManager := content.GetContentManager()
	contentManager.AddPageLayout(pageLayoutTemplate)
	contentManager.AddPageComponent(messageComponentTemplate)
	contentManager.AddPageComponent(consoleMessageComponentTemplate)

	contentManager.HomeURL = homeURL
	contentManager.ErrorHandler = m.RedirectToError

	contentManager.AuthURL = authURL

	return nil
}
