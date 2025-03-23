package module_site

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/services/logger"
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
		content.HandleNotFound(w, r)
		return
	}

	welcomeMessage := "Welcome!"
	u := user.GetUserFromContext(r.Context())
	if u != nil {
		welcomeMessage = "Welcome " + u.Email + "!"
	}

	homepage := content.NewManagedContent(r)
	homepage.AddContent(homeContent)
	homepage.Messages, _ = messages.GetMessages(r, w)
	homepage.Title = welcomeMessage

	err := homepage.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "there was an error rendering the home page", err, http.StatusInternalServerError)
	}
}

// Login provides a handler to act as a portal to user authentication
func (m *SiteModule) Login(w http.ResponseWriter, r *http.Request) {
	login := content.NewManagedContent(r)
	login.Messages, _ = messages.GetMessages(r, w)
	login.Title = "Login"

	login.AddContent(loginContent)

	err := login.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "there was an error rendering the login page", err, http.StatusInternalServerError)
		return
	}
}

// Register provides a handler to act as a portal to user registration
func (m *SiteModule) Register(w http.ResponseWriter, r *http.Request) {
	register := content.NewManagedContent(r)
	register.Messages, _ = messages.GetMessages(r, w)
	register.Title = "Register"

	register.AddContent(registerContent)

	err := register.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "there was an error rendering the register page", err, http.StatusInternalServerError)
		return
	}
}

// Error is the handler for the /error endpoint
func (m *SiteModule) Error(w http.ResponseWriter, r *http.Request) {
	contentMessages, messagesErr := messages.GetMessages(r, w)
	if messagesErr != nil {
		logger.LogRequestError(r, messagesErr)
	}

	var errorMessages []messages.ContentMessage
	for _, c := range contentMessages {
		if c.IsError() {
			errorMessages = append(errorMessages, c)
		}
	}

	errorPage := content.NewManagedContent(r)
	errorPage.Title = "Error"
	errorPage.AddContent(errorContent)
	errorPage.Data = errorMessages

	if err := errorPage.Render(w, r); err != nil {
		content.HandleError(w, r, "there was an error rendering the error page", err, http.StatusInternalServerError)
	}
}

// HandleError writes the specified error to the messages store and redirects the
// requestor to the error URL
func (m *SiteModule) HandleError(w http.ResponseWriter, r *http.Request, message string, err error, httpStatus int) {
	errAddMessage := messages.AddErrorMessage(w, r, message)
	if errAddMessage != nil {
		logger.LogRequestError(r, fmt.Errorf("there was an error adding an error message before redirecting to the error page: %w", errAddMessage))
	}

	m.Error(w, r)
}

// SessionExample provides a handler that exercises session code by creating a session to store
// a visit counter that will keep track of how many visits a particular user has made to
// the URL this handler serves.
func (m *SiteModule) SessionExample(w http.ResponseWriter, r *http.Request) {
	const counterKey = "counter"

	// get the "counterSession" session
	sessionManager := session.GetSessionManager()
	if sessionManager == nil {
		content.HandleError(w, r, "there was an error retrieving the session manager", session.NewErrSessionManagerNotFound(nil), http.StatusInternalServerError)
		return
	}

	mySession, sessionManagerErr := sessionManager.Get(r, "counterSession")
	if sessionManagerErr != nil || mySession == nil {
		content.HandleError(w, r, "there was an error retrieving the session", sessionManagerErr, http.StatusInternalServerError)
		return
	}

	// get the existing counter value from the session data
	counter, atoiError := strconv.Atoi(mySession.Data[counterKey])
	if atoiError != nil {
		counter = 1
	}

	page := content.NewManagedContent(r)
	page.AddContent(homeContent)
	page.Title = fmt.Sprintf("# of My Visits: %d", counter)

	// increment the counter and convert the value to a string
	sCounter := fmt.Sprintf("%d", counter+1)

	// save the updated counter value in the session
	mySession.Data[counterKey] = sCounter
	sessionSaveErr := mySession.Save(r, w)
	if sessionSaveErr != nil {
		content.HandleError(w, r, "unable to save the counter session", sessionSaveErr, http.StatusInternalServerError)
		return
	}

	// render the result
	renderErr := page.Render(w, r)
	if renderErr != nil {
		content.HandleError(w, r, "there was an error rendering the home page", renderErr, http.StatusInternalServerError)
		return
	}
}

// NotFound handles the /404 endpoint by displaying the "NotFound" page
func (m *SiteModule) NotFound(w http.ResponseWriter, r *http.Request) {
	notFound := content.NewManagedContent(r)
	notFound.AddContent(notFoundContent)
	notFound.Data = fmt.Sprintf("The resource you requested was not found: %s", r.URL.Path)
	notFound.Title = "Resource Not Found"
	notFound.ResponseStatusCode = http.StatusNotFound

	err := notFound.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "there was an error rendering the home page", err, http.StatusInternalServerError)
	}
}

// Logout redirects the "/logout" route to the logout functionality provides by the auth package
func (m *SiteModule) Logout(w http.ResponseWriter, r *http.Request) {
	content.RedirectToURL(w, r, "/auth/logout")
}
