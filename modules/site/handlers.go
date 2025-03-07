package module_site

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/user"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// Home renders the homepage and handles the following routes:
func (m *SiteModule) Home(w http.ResponseWriter, r *http.Request) {
	homepage := content.NewManagedContent(r)
	homepage.Site = m.Title
	homepage.Title = "Home"
	homepage.AddContentTemplate(homeContent)
	err := homepage.Render(w, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("There was an error rendering the specified content: %s", err.Error()), http.StatusInternalServerError)
	}
}

// Error is meant to provide a default error handler for the module that can be
// used as the default error handler for the entire application
func (m *SiteModule) Error(w http.ResponseWriter, r *http.Request, code int, message string) {
	if code == 0 {
		code = http.StatusInternalServerError
	}

	errorPage := content.NewManagedContent(r)
	errorPage.Site = m.Title
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

// ServeFavicon serves the favicon resource to a requestor
func (m *SiteModule) ServeFavicon(w http.ResponseWriter, r *http.Request) {
	faviconPath := filepath.Join("modules", "site", "templates", "html", "img", "favicon.ico")
	http.ServeFile(w, r, faviconPath)
}

// SessionTest is a method used for testing session functionality and will be removed
func (m *SiteModule) SessionTest(w http.ResponseWriter, r *http.Request) {
	session, sessionErr := m.sessionManager.Get(r, "cookie") // "cookie" for CookieStore, "visit" for SQLiteStore
	if sessionErr != nil {
		errMessage := fmt.Sprintf("session error: %s", sessionErr.Error())
		content.RenderError(w, r, http.StatusInternalServerError, errMessage)
		return
	}

	var userEmail string
	var hs_user *user.User
	var userErr error

	userID := session.Data["user_id"]
	if userID != "" {
		hs_user, userErr = user.GetUserByID(m.Database, userID)
		if userErr != nil {
			errMessage := fmt.Sprintf("user retrieval error")
			content.RenderError(w, r, http.StatusInternalServerError, errMessage)
			return
		}
	}

	if hs_user != nil {
		userEmail = hs_user.Email
	} else {
		userEmail = "No User"

		hs_user = user.NewUser()
		hs_user.Email = "trentnix@gmail.com"
		userErr = hs_user.Save(m.Database)
		if userErr != nil {
			errMessage := fmt.Sprintf("user creation error: %v", userErr)
			content.RenderError(w, r, http.StatusInternalServerError, errMessage)
			return
		}

		session.Data["user_id"] = hs_user.ID
		sessionErr := session.Save(r, w)
		if sessionErr != nil {
			errMessage := fmt.Sprintf("session error: %s", sessionErr.Error())
			content.RenderError(w, r, http.StatusInternalServerError, errMessage)
			return
		}
	}

	homepage := content.NewManagedContent(r)
	homepage.Site = m.Title
	homepage.Title = userEmail
	homepage.AddContentTemplate(homeContent)
	err := homepage.Render(w, r)
	if err != nil {
		errMessage := fmt.Sprintf("There was an error rendering the specified content: %s", err.Error())
		content.RenderError(w, r, http.StatusInternalServerError, errMessage)
		return
	}
}

// Login provides a handler to test login functionality - currently used for testing
func (m *SiteModule) Login(w http.ResponseWriter, r *http.Request) {
	login := content.NewManagedContent(r)
	login.Site = m.Title
	login.Title = "Login"

	login.AddContentTemplate(content.Template("modules/site/templates/html/login.html"))

	err := login.Render(w, r)
	if err != nil {
		errMessage := fmt.Sprintf("There was an error rendering the specified content: %s", err.Error())
		content.RenderError(w, r, http.StatusInternalServerError, errMessage)
		return
	}
}
