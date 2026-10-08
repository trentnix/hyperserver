// Package module_site provides the development site's pages, layouts, and sample routes.
// Importing it registers a shared SiteModule. Production applications must not
// import this module because it exposes diagnostic and sample handlers.
package module_site

import (
	"context"
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type (
	// ContactRepository stores a submission and sets its ID and creation time only
	// after a successful write. Implementations must not close borrowed resources.
	ContactRepository interface {
		Create(context.Context, *models.ContactSubmission) error
	}

	// SiteModule contains all of the data required to implement the site module
	SiteModule struct {
		AppName string
		// NewContacts optionally selects and prepares this module's contact storage.
		// Set it before initialization. A nil factory uses the shared SQLite pool.
		NewContacts func(context.Context) (ContactRepository, error)

		contacts            ContactRepository
		sessionManager      *session.SessionManager
		contentManager      *content_services.ContentManagerService
		mailClient          *messaging.MailClient
		registrationEnabled bool
		rateLimit           *config.ModuleRateLimitConfig
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
func (m *SiteModule) Init(ctx context.Context, s *server.ApplicationServer) error {
	var contacts ContactRepository
	var err error
	if m.NewContacts != nil {
		contacts, err = m.NewContacts(ctx)
	} else {
		contacts, err = models.NewContactRepository(ctx, s.Database)
	}
	if err != nil {
		return err
	}
	if contacts == nil {
		return fmt.Errorf("site contact repository is not configured")
	}

	m.AppName = s.Config.App.Name
	m.rateLimit = s.Config.App.SiteRateLimit

	m.contacts = contacts
	m.sessionManager = s.SessionManager
	m.contentManager = s.ContentManager
	m.mailClient = s.Mail
	m.registrationEnabled = s.Config.Auth.Enabled && s.Config.Auth.RegistrationEnabled
	m.contentManager.AddPageLayout(pageLayoutTemplate)
	m.contentManager.AddHtmxLayout(partialLayoutTemplate)
	m.contentManager.AddPageComponent(navComponentTemplate)
	m.contentManager.AddPageComponent(notificationsComponentTemplate)
	m.contentManager.AddPageComponent(consoleMessagesComponentTemplate)
	authEnabled := s.Config.Auth.Enabled
	m.contentManager.BuildLayoutData = func(r *http.Request) any {
		return struct {
			AuthEnabled   bool
			Authenticated bool
		}{authEnabled, user.GetUserFromContext(r.Context()) != nil}
	}

	m.contentManager.HomeURL = homeURL
	m.contentManager.AuthURL = authURL
	m.contentManager.ErrorURL = errorURL
	m.contentManager.NotFoundURL = notFoundURL
	m.contentManager.HandleMessage = m.HandleMessage
	m.contentManager.HandleError = m.HandleError
	m.contentManager.HandleNotFound = m.NotFound

	return nil
}
