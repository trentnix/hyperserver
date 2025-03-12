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

	homepage.Messages = content.RetrieveMessages(r, w)
	homepage.Site = m.AppName
	homepage.Title = welcomeMessage

	err := homepage.Render(w, r)
	if err != nil {
		renderingError := fmt.Sprintf("Could not render the home page: %s", err.Error())
		logger.LogRequestError(r, renderingError, err)
		content.RenderError(w, r, http.StatusInternalServerError, renderingError)
	}
}

// Login provides a handler to test login functionality - currently used for testing
func (m *SiteModule) Login(w http.ResponseWriter, r *http.Request) {
	login := content.NewManagedContent(r)

	login.Messages = content.RetrieveMessages(r, w)
	login.Site = m.AppName
	login.Title = "Login"

	login.AddContentTemplate(loginContent)

	err := login.Render(w, r)
	if err != nil {
		errMessage := fmt.Sprintf("There was an error rendering the specified content: %s", err.Error())
		content.RenderError(w, r, http.StatusInternalServerError, errMessage)
		return
	}
}

// Error is meant to provide a default error handler for the module that can be
// used as the default error handler for the entire application
func (m *SiteModule) Error(w http.ResponseWriter, r *http.Request, code int, message string) {
	if code == 0 {
		code = http.StatusInternalServerError
	}

	errorPage := content.NewManagedContent(r)
	errorPage.Site = m.AppName
	errorPage.Title = "Error"
	errorPage.AddContentTemplate(errorContent)
	errorPage.Data = message

	w.WriteHeader(code)
	if err := errorPage.Render(w, r); err != nil {
		// given that this is an error handler intended to function as the default error
		// handler for the application, this is the fallback in the case where the
		// error content is unable to render
		renderingError := fmt.Sprintf("Could not render the error page: %s", err.Error())
		logger.LogRequestError(r, renderingError, err)
		http.Error(w, renderingError, http.StatusInternalServerError)
	}
}
