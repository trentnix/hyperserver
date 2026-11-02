package auth

import (
	"errors"
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const (
	authLoginSelectionTemplate = "templates/html/pages/login-selection.html"
	authLoginDefaultTemplate   = "templates/html/pages/login-default.html"
	authLoginSelectionPartial  = "auth.page.login.selection"
	authLoginDefaultPartial    = "auth.page.login.default"
	authEndpoint               = "/auth/login"
)

// GetLogin renders the various authentication options to a user trying to login
// to the application. If there is only a single authentication option, the user is
// sent directly to the login page of that authentication service.
func (a *AuthManager) GetLogin(w http.ResponseWriter, r *http.Request) {
	c := content.NewManagedContent(r, a.contentManager)

	var loginHTML []template.HTML

	var authServices []LoginHandler
	for _, service := range a.Services.Services() {
		if handler, ok := service.(LoginHandler); ok {
			authServices = append(authServices, handler)
		}
	}
	if len(authServices) == 0 {
		http.NotFound(w, r)
		return
	}
	if len(authServices) == 1 {
		// there is only 1 auth service - display the default
		c.PartialName = authLoginDefaultPartial
		c.Templates = a.templates[authLoginDefaultTemplate]
		c.Data = "/auth/login/" + authServices[0].AuthType()
	} else {
		c.PartialName = authLoginSelectionPartial
		for _, service := range authServices {
			loginHTML = append(loginHTML, service.GetLoginButton())
		}

		c.Templates = a.templates[authLoginSelectionTemplate]
		c.Data = loginHTML
	}

	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "could not render the login page", err)
	}
}

// GetLoginService retrieves the first step of login process for the given AuthService
// (specified by authType)
func (a *AuthManager) GetLoginService(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	a.GetSpecificLoginService(w, r, authType)
}

// GetSpecificLoginService retrieves the specified AuthService and redirects the requestor to
// the specified service's login entry point
func (a *AuthManager) GetSpecificLoginService(w http.ResponseWriter, r *http.Request, authType string) {
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to login.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(LoginHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	authService.GetLogin(w, r)
}

// Login starts the login process for the given AuthService (specified by authType)
func (a *AuthManager) Login(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to login.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(LoginHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	u := authService.Login(w, r)
	if u == nil {
		logger.Audit(r.Context(), "login", "rejected")
		return
	}

	// Load the redirect session before issuing an authenticated session.
	s, err := session.Get(r, session.AuthSession)
	if err != nil {
		logger.LogRequestError(r, err)
		if errors.Is(err, session.ErrInvalidToken) {
			session.ExpireCookie(w, r, session.AuthSession)
			util.HttpError(w, r, "Invalid session cookie", nil, http.StatusBadRequest)
			return
		}
		util.HttpError(w, r, "Unable to load the login session", nil, http.StatusInternalServerError)
		return
	}

	setAuthenticatedUserErr := identity.SetAuthenticatedUser(w, r, u)
	if setAuthenticatedUserErr != nil {
		logger.Audit(r.Context(), "login", "failed")
		logger.LogRequestError(r, setAuthenticatedUserErr)
		util.HttpError(w, r, "There was an internal error when trying to login", nil, http.StatusInternalServerError)
		return
	}

	logger.Audit(r.Context(), "login", "succeeded")
	r = user.AddUserToRequestContext(r, u)
	err = flash.AddSuccessNotification(w, r, "You have been successfully logged in")
	if err != nil {
		logger.LogRequestError(r, errors.New("login succeeded but its notification could not be saved"))
	}

	redirectURL := ""
	if !s.IsNew {
		msg, ok := s.Data[session.RedirectURL]
		if ok {
			sMessage, ok := msg.(string)
			if ok {
				redirectURL = sMessage
			}
		}
	}

	redirectURL = util.SafeRedirectURL(redirectURL, a.contentManager.HomeURL)
	if u.NeedsVerification() {
		redirectURL = "/auth/request/verify"
	}

	// clear the redirect session, it is no longer valid
	if err := s.End(w, r); err != nil {
		logger.LogRequestError(r, errors.New("login succeeded but the redirect session could not be cleared"))
	}

	util.RedirectToURL(w, r, redirectURL)
}

// Logout ends a session for any logged-in user
func (a *AuthManager) Logout(w http.ResponseWriter, r *http.Request) {
	logoutErr := identity.LogoutAuthenticatedUser(w, r)
	if logoutErr != nil {
		logger.Audit(r.Context(), "logout", "failed")
		logger.LogRequestError(r, logoutErr)
		util.HttpError(w, r, "There was an error trying to log out", nil, http.StatusInternalServerError)
		return
	}

	logger.Audit(r.Context(), "logout", "succeeded")
	err := flash.AddSuccessNotification(w, r, "You have been successfully logged out")
	if err != nil {
		logger.LogRequestError(r, err)
	}

	util.RedirectToURL(w, r, a.contentManager.HomeURL)
}
