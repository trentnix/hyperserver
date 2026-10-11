// Package module_site provides the development site's pages, layouts, and sample routes.
// Importing it registers a SiteModule factory. Production applications must not
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
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
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
		// NewContacts prepares this module's contact storage. Applications must
		// supply the factory before initialization.
		NewContacts func(context.Context) (ContactRepository, error)

		contacts            ContactRepository
		contentManager      *content_services.ContentManagerService
		mailClient          *messaging.MailClient
		registrationEnabled bool
		rateLimit           *config.ModuleRateLimitConfig
	}
)

const (
	module                           = "module_site"
	pageLayoutTemplate               = "templates/html/layouts/site.html"
	partialLayoutTemplate            = "templates/html/layouts/partial.html"
	navComponentTemplate             = "templates/html/components/nav.html"
	notificationsComponentTemplate   = "templates/html/components/notifications.html"
	consoleMessagesComponentTemplate = "templates/html/components/console-messages.html"
	homePageTemplate                 = "templates/html/pages/home.html"
	loginPageTemplate                = "templates/html/pages/login.html"
	registerPageTemplate             = "templates/html/pages/register.html"
	errorPageTemplate                = "templates/html/pages/error.html"
	messagePageTemplate              = "templates/html/pages/message.html"
	notFoundPageTemplate             = "templates/html/pages/not-found.html"
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

// init makes the module available without creating application state.
func init() {
	handlers.Register(Module(nil, nil))
}

// Module describes the site with an application-supplied contact-storage factory.
// The factory runs during Init, not registration. A nil factory prevents initialization.
// Mail is optional. Without a client, the test-email route reports delivery unavailable.
// Applications own shared services and any connections supplied by the factory.
func Module(newContacts func(context.Context) (ContactRepository, error), mail *messaging.MailClient) handlers.Descriptor {
	return handlers.Descriptor{Name: "site", New: func() handlers.Handler {
		return &SiteModule{NewContacts: newContacts, mailClient: mail}
	}}
}

// Init validates required services, prepares contact storage, and configures the
// site's rendering. It does not initialize or close borrowed application services.
func (m *SiteModule) Init(ctx context.Context, s *server.ApplicationServer) error {
	if s.Config == nil {
		return fmt.Errorf("site requires application configuration")
	}
	if s.ContentManager == nil {
		return fmt.Errorf("site requires a content manager")
	}

	if m.NewContacts == nil {
		return fmt.Errorf("site requires a contact-storage factory")
	}
	contacts, err := m.NewContacts(ctx)
	if err != nil {
		return err
	}
	if contacts == nil {
		return fmt.Errorf("site contact repository is not configured")
	}

	m.AppName = s.Config.App.Name
	m.rateLimit = s.Config.App.SiteRateLimit

	m.contacts = contacts
	m.contentManager = s.ContentManager
	if s.Config.App.RenderNotifications && m.contentManager.LoadNotifications == nil {
		m.contentManager.LoadNotifications = flash.GetNotifications
	}
	m.registrationEnabled = s.Config.Auth.Enabled && s.Config.Auth.RegistrationEnabled
	m.contentManager.AddPageLayout(siteModuleTemplatesFS, pageLayoutTemplate)
	m.contentManager.AddHtmxLayout(siteModuleTemplatesFS, partialLayoutTemplate)
	m.contentManager.AddPageComponent(siteModuleTemplatesFS, navComponentTemplate)
	m.contentManager.AddPageComponent(siteModuleTemplatesFS, notificationsComponentTemplate)
	m.contentManager.AddPageComponent(siteModuleTemplatesFS, consoleMessagesComponentTemplate)
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
