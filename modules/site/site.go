// site.go defines and initializes the site module
package module_site

import (
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	// SiteModule contains all of the data required to implement the site module
	SiteModule struct {
		AppName  string
		Database *sqlx.DB

		sessionManager *session.SessionManager
		contentManager *content_services.ContentManagerService
	}
)

const (
	module                          = "module_site"
	pageLayoutTemplate              = "modules/site/templates/html/layouts/site.html"
	messageComponentTemplate        = "modules/site/templates/html/components/notifications.html"
	consoleMessageComponentTemplate = "modules/site/templates/html/components/console-messages.html"
	homeContent                     = "modules/site/templates/html/index.html"
	loginContent                    = "modules/site/templates/html/login.html"
	registerContent                 = "modules/site/templates/html/register.html"
	errorContent                    = "modules/site/templates/html/error.html"
	messageContent                  = "modules/site/templates/html/message.html"
	notFoundContent                 = "modules/site/templates/html/not-found.html"

	homeURL     = "/"
	authURL     = "/login"
	registerURL = "/register"
	errorURL    = "/error"
	notFoundURL = "/404"
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
	m.sessionManager = s.SessionManager
	m.contentManager = s.ContentManager
	m.contentManager.AddPageLayout(pageLayoutTemplate)
	m.contentManager.AddPageComponent(messageComponentTemplate)
	m.contentManager.AddPageComponent(consoleMessageComponentTemplate)

	m.contentManager.HomeURL = homeURL
	m.contentManager.AuthURL = authURL
	m.contentManager.ErrorURL = errorURL
	m.contentManager.NotFoundURL = notFoundURL
	m.contentManager.HandleMessage = m.HandleMessage
	m.contentManager.HandleError = m.HandleError
	m.contentManager.HandleNotFound = m.NotFound

	return nil
}
