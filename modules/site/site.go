// site.go defines and initializes the site module
package module_site

import (
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	// SiteModule contains all of the data required to implement the site module
	SiteModule struct {
		AppName  string
		Database *sqlx.DB

		sessionManager *session.SessionManager
		contentManager *content_services.ContentManagerService
		mailClient     *messaging.MailClient
	}
)

const (
	module                           = "module_site"
	pageLayoutTemplate               = "modules/site/templates/html/layouts/site.html"
	partialLayoutTemplate            = "modules/site/templates/html/layouts/partial.html"
	navComponentTemplate             = "modules/site/templates/html/components/nav.html"
	notificationsComponentTemplate   = "modules/site/templates/html/components/notifications.html"
	consoleMessagesComponentTemplate = "modules/site/templates/html/components/console-messages.html"
	homePageTemplate                 = "modules/site/templates/html/pages/home.html"
	loginPageTemplate                = "modules/site/templates/html/pages/login.html"
	registerPageTemplate             = "modules/site/templates/html/pages/register.html"
	errorPageTemplate                = "modules/site/templates/html/pages/error.html"
	messagePageTemplate              = "modules/site/templates/html/pages/message.html"
	notFoundPageTemplate             = "modules/site/templates/html/pages/not-found.html"
	homePagePartialName              = "site.page.home"
	loginPagePartialName             = "site.page.login"
	registerPagePartialName          = "site.page.register"
	errorPagePartialName             = "site.page.error"
	messagePagePartialName           = "site.page.message"
	notFoundPagePartialName          = "site.page.not_found"
	contactPagePartialName           = "site.page.contact"
	contactFormPartialName           = "site.partial.contact.form"

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
	m.mailClient = s.Mail
	m.contentManager.AddPageLayout(pageLayoutTemplate)
	m.contentManager.AddHtmxLayout(partialLayoutTemplate)
	m.contentManager.AddPageComponent(navComponentTemplate)
	m.contentManager.AddPageComponent(notificationsComponentTemplate)
	m.contentManager.AddPageComponent(consoleMessagesComponentTemplate)

	m.contentManager.HomeURL = homeURL
	m.contentManager.AuthURL = authURL
	m.contentManager.ErrorURL = errorURL
	m.contentManager.NotFoundURL = notFoundURL
	m.contentManager.HandleMessage = m.HandleMessage
	m.contentManager.HandleError = m.HandleError
	m.contentManager.HandleNotFound = m.NotFound

	return nil
}
