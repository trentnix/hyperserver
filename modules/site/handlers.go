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
	r = content.AddUserSuccessMessage(r, "Success loading the homepage!")
	err := homepage.Render(w, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("There was an error rendering the specified content: %s", err.Error()), http.StatusInternalServerError)
	}
}

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

// Home renders the homepage and handles the following routes:
func (m *SiteModule) SessionTest(w http.ResponseWriter, r *http.Request) {
	session, sessionErr := m.sessionManager.Get(r, "cookie") // "cookie" for CookieStore, "visit" for SQLiteStore
	if sessionErr != nil {
		http.Error(w, fmt.Sprintf("session error: %s", sessionErr.Error()), http.StatusInternalServerError)
		return
	}

	var userEmail string
	var hs_user *user.User
	var userErr error

	userID := session.Data["user_id"]
	if userID != "" {
		hs_user, userErr = user.GetUserByID(m.Database, userID)
		if userErr != nil {
			http.Error(w, fmt.Sprintf("user retrieval error"), http.StatusInternalServerError)
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
			http.Error(w, fmt.Sprintf("user creation error: %v", userErr), http.StatusInternalServerError)
			return
		}

		session.Data["user_id"] = hs_user.ID
		sessionErr := session.Save(r, w)
		if sessionErr != nil {
			http.Error(w, fmt.Sprintf("session error: %s", sessionErr.Error()), http.StatusInternalServerError)
			return
		}
	}

	homepage := content.NewManagedContent(r)
	homepage.Site = m.Title
	homepage.Title = userEmail
	homepage.AddContentTemplate(homeContent)
	err := homepage.Render(w, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("There was an error rendering the specified content: %s", err.Error()), http.StatusInternalServerError)
		return
	}
}

// Login provides a handler to test login functionality
func (m *SiteModule) Login(w http.ResponseWriter, r *http.Request) {
	login := content.NewManagedContent(r)
	login.Site = m.Title
	login.Title = "Login"

	login.AddContentTemplate(content.Template("modules/site/templates/html/login.html"))

	err := login.Render(w, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("There was an error rendering the specified content: %s", err.Error()), http.StatusInternalServerError)
		return
	}
}
