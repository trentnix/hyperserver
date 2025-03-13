package module_site

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/user"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// ServeFavicon serves the favicon resource to a requestor
func (m *SiteModule) ServeFavicon(w http.ResponseWriter, r *http.Request) {
	faviconPath := filepath.Join("modules", "site", "templates", "html", "img", "favicon.ico")
	http.ServeFile(w, r, faviconPath)
}

// Home renders the homepage
func (m *SiteModule) Home(w http.ResponseWriter, r *http.Request) {
	welcomeMessage := "Welcome!"
	hs_user := user.GetUserFromContext(r.Context())
	if hs_user != nil {
		welcomeMessage = "Welcome " + hs_user.Email + "!"
	}

	homepage := content.NewManagedContent(r)
	homepage.AddContentTemplate(homeContent)
	homepage.Messages, _ = content.RetrieveMessages(r, w)
	homepage.Title = welcomeMessage

	err := homepage.Render(w, r)
	if err != nil {
		errMessage := fmt.Sprintf("There was an error rendering the homepage: %s", err.Error())
		content.HandleError(w, r, errMessage, err)
	}
}

// Login provides a handler to test login functionality - currently used for testing
func (m *SiteModule) Login(w http.ResponseWriter, r *http.Request) {
	login := content.NewManagedContent(r)
	login.Messages, _ = content.RetrieveMessages(r, w)
	login.Title = "Login"

	login.AddContentTemplate(loginContent)

	err := login.Render(w, r)
	if err != nil {
		errMessage := fmt.Sprintf("There was an error rendering the login page: %s", err.Error())
		content.HandleError(w, r, errMessage, err)
		return
	}
}

// RedirectToError writes the specified error to the messages store and redirects the
// requestor to the error URL
func (m *SiteModule) RedirectToError(w http.ResponseWriter, r *http.Request, message string, e error) {
	if e != nil {
		logger.LogRequestError(r, message, e)
	}

	content.AddErrorMessage(w, r, message)

	url := errorURL
	contentManager := content.GetContentManager()
	if contentManager != nil {
		url = contentManager.ErrorURL
	}

	content.RedirectToURL(w, r, url)
}

// Error is the handler for the /error endpoint
func (m *SiteModule) Error(w http.ResponseWriter, r *http.Request) {
	contentMessages, messagesErr := content.RetrieveMessages(r, w)
	if messagesErr != nil {
		// there was an error - create a new error to display for the user
		message := fmt.Sprintf("there was an error retrieving content messages: %s", messagesErr.Error())
		errContentMessage := content.NewContentMessage(message, content.MessageTypeError)
		contentMessages = append(contentMessages, errContentMessage)

		logger.LogRequestError(r, message, messagesErr)
	}

	errorPage := content.NewManagedContent(r)
	errorPage.Title = "Error"
	errorPage.AddContentTemplate(errorContent)
	errorPage.Data = contentMessages

	if err := errorPage.Render(w, r); err != nil {
		content.HandleRenderingError(w, r, "could not render the login page", err)
	}
}
