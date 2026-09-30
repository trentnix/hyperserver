package module_site

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// ServeFavicon serves the favicon resource to a requestor
func (m *SiteModule) ServeFavicon(w http.ResponseWriter, r *http.Request) {
	faviconPath := filepath.Join("modules", "site", "templates", "html", "img", "favicon.ico")
	http.ServeFile(w, r, faviconPath)
}

// Home renders the homepage
func (m *SiteModule) Home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != homeURL {
		m.contentManager.HandleNotFound(w, r)
		return
	}

	welcomeMessage := "Welcome!"
	u := user.GetUserFromContext(r.Context())
	if u != nil {
		welcomeMessage = "Welcome " + u.Email + "!"
	}

	homepage := content.NewManagedContent(r, m.contentManager)
	homepage.PartialName = homePagePartialName
	homepage.AddContent(homePageTemplate)
	homepage.Title = welcomeMessage

	err := homepage.Render(w, r)
	if err != nil {
		m.contentManager.HandleError(w, r, "there was an error rendering the home page", err, http.StatusInternalServerError)
	}
}

// Login provides a handler to act as a portal to user authentication
func (m *SiteModule) Login(w http.ResponseWriter, r *http.Request) {
	login := content.NewManagedContent(r, m.contentManager)
	login.PartialName = loginPagePartialName
	login.Title = "Login"

	login.AddContent(loginPageTemplate)

	err := login.Render(w, r)
	if err != nil {
		m.contentManager.HandleError(w, r, "there was an error rendering the login page", err, http.StatusInternalServerError)
		return
	}
}

// Register provides a handler to act as a portal to user registration
func (m *SiteModule) Register(w http.ResponseWriter, r *http.Request) {
	if !m.registrationEnabled {
		auth.RegistrationUnavailable(w, r)
		return
	}

	register := content.NewManagedContent(r, m.contentManager)
	register.PartialName = registerPagePartialName
	register.Title = "Register"

	register.AddContent(registerPageTemplate)

	err := register.Render(w, r)
	if err != nil {
		m.contentManager.HandleError(w, r, "there was an error rendering the register page", err, http.StatusInternalServerError)
		return
	}
}

// Error renders queued error notifications. It currently returns HTTP 200.
func (m *SiteModule) Error(w http.ResponseWriter, r *http.Request) {
	notifications, messagesErr := messages.GetNotifications(w, r)
	if messagesErr != nil {
		logger.LogRequestError(r, messagesErr)
	}

	var errorMessages []messages.Notification
	for _, n := range notifications {
		if n.IsError() {
			errorMessages = append(errorMessages, n)
		}
	}

	errorPage := content.NewManagedContent(r, m.contentManager)
	errorPage.PartialName = errorPagePartialName
	errorPage.Title = "Error"
	errorPage.AddContent(errorPageTemplate)
	errorPage.Data = errorMessages

	if err := errorPage.Render(w, r); err != nil {
		m.contentManager.HandleError(w, r, "there was an error rendering the error page", err, http.StatusInternalServerError)
	}
}

// HandleError queues message and renders the error page directly.
// It currently ignores err and httpStatus, so the response normally remains HTTP 200.
func (m *SiteModule) HandleError(w http.ResponseWriter, r *http.Request, message string, err error, httpStatus int) {
	errAddMessage := messages.AddErrorNotification(w, r, message)
	if errAddMessage != nil {
		logger.LogRequestError(r, fmt.Errorf("there was an error adding an error message before redirecting to the error page: %w", errAddMessage))
	}

	m.Error(w, r)
}

// HandleMessage renders message as trusted HTML. The caller must escape untrusted values.
func (m *SiteModule) HandleMessage(w http.ResponseWriter, r *http.Request, message string) {
	if message == "" {
		message = "(no message)"
	}

	messagePage := content.NewManagedContent(r, m.contentManager)
	messagePage.PartialName = messagePagePartialName
	messagePage.AddContent(messagePageTemplate)
	messagePage.Data = template.HTML(message)
	err := messagePage.Render(w, r)
	if err != nil {
		m.contentManager.HandleError(w, r,
			fmt.Sprintf("Unable to display the following message: %s", message),
			err,
			http.StatusInternalServerError)
	}
}

// SessionExample provides a handler that exercises session code by creating a session to store
// a visit counter that will keep track of how many visits a particular user has made to
// the URL this handler serves.
func (m *SiteModule) SessionExample(w http.ResponseWriter, r *http.Request) {
	const counterKey = "counter"

	mySession, sessionManagerErr := session.Get(r, "counterSession")
	if sessionManagerErr != nil || mySession == nil {
		m.contentManager.HandleError(w, r, "there was an error retrieving the session", sessionManagerErr, http.StatusInternalServerError)
		return
	}

	counter, _ := mySession.Data[counterKey].(int)
	counter++ // add this visit

	page := content.NewManagedContent(r, m.contentManager)
	page.PartialName = homePagePartialName
	page.AddContent(homePageTemplate)
	page.Title = fmt.Sprintf("# of My Visits: %d", counter)

	// save the updated counter value in the session
	mySession.Data[counterKey] = counter
	sessionSaveErr := mySession.Save(w, r)
	if sessionSaveErr != nil {
		m.contentManager.HandleError(w, r, "unable to save the counter session", sessionSaveErr, http.StatusInternalServerError)
		return
	}

	// render the result
	renderErr := page.Render(w, r)
	if renderErr != nil {
		m.contentManager.HandleError(w, r, "there was an error rendering the home page", renderErr, http.StatusInternalServerError)
		return
	}
}

// NotFound handles the /404 endpoint by displaying the "NotFound" page
func (m *SiteModule) NotFound(w http.ResponseWriter, r *http.Request) {
	notFound := content.NewManagedContent(r, m.contentManager)
	notFound.PartialName = notFoundPagePartialName
	notFound.AddContent(notFoundPageTemplate)
	notFound.Data = fmt.Sprintf("The resource you requested was not found: %s", r.URL.Path)
	notFound.Title = "Resource Not Found"
	notFound.ResponseStatusCode = http.StatusNotFound

	err := notFound.Render(w, r)
	if err != nil {
		m.contentManager.HandleError(w, r, "there was an error rendering the home page", err, http.StatusInternalServerError)
	}
}

// Logout redirects the "/logout" route to the logout functionality provides by the auth package
func (m *SiteModule) Logout(w http.ResponseWriter, r *http.Request) {
	util.RedirectToURL(w, r, "/auth/logout")
}
