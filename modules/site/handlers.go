package module_site

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
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

// HandleMessage renders message as text escaped by the page template.
func (m *SiteModule) HandleMessage(w http.ResponseWriter, r *http.Request, message string) {
	if message == "" {
		message = "(no message)"
	}

	messagePage := content.NewManagedContent(r, m.contentManager)
	messagePage.PartialName = messagePagePartialName
	messagePage.AddContent(messagePageTemplate)
	messagePage.Data = message
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

	var counter int
	if err := mySession.DecodeValue(counterKey, &counter); err != nil {
		m.contentManager.HandleError(w, r, "unable to decode the counter session", err, http.StatusInternalServerError)
		return
	}
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

// Logout forwards the site's POST to authentication logout without changing its method.
func (m *SiteModule) Logout(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/auth/logout", http.StatusTemporaryRedirect)
}
