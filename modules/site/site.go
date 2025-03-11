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
		Title    string
		Database *sqlx.DB

		sessionManager *session.SessionManager
	}
)

const (
	module                   = "module_site"
	pageLayoutTemplate       = "modules/site/templates/html/layouts/site.html"
	messageComponentTemplate = "modules/site/templates/html/components/message.html"
	homeContent              = "modules/site/templates/html/index.html"
	errorContent             = "modules/site/templates/html/error.html"

	homeURL = "/"
	authURL = "/login"
)

// init registers an instance of SiteModule with the application handlers. init runs
// when the module_site
func init() {
	handlers.Register(new(SiteModule))
}

// Init takes care of initializing the specified SiteModule instance
func (m *SiteModule) Init(s *server.ApplicationServer) error {
	m.Title = s.Config.App.Title

	m.Database = s.Database
	m.sessionManager = session.GetSessionManager()

	contentManager := content.GetContentManager()
	contentManager.AddPageLayoutTemplate(content.Template(pageLayoutTemplate))
	contentManager.AddPageComponentTemplate(content.Template(messageComponentTemplate))
	contentManager.HomeURL = homeURL
	contentManager.HomeHandler = m.Home
	contentManager.ErrorHandler = m.Error

	contentManager.AuthURL = authURL

	return nil
}
