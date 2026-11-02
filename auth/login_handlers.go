package auth

import (
	"context"
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
	authLoginSelectionPartial  = "auth.page.login.selection"
	authEndpoint               = "/auth/login"
)

// GetLogin renders the various authentication options to a user trying to login
// to the application. If there is only a single authentication option, the user is
// sent directly to the login page of that authentication service.
func (a *AuthManager) GetLogin(w http.ResponseWriter, r *http.Request) {
	authServices, err := a.loginChoices(r.Context())
	if err != nil {
		a.contentManager.HandleError(w, r, "Login options are temporarily unavailable. Please try again.", err, http.StatusServiceUnavailable)
		return
	}
	if len(authServices) == 0 {
		http.NotFound(w, r)
		return
	}
	if len(authServices) == 1 {
		authServices[0].GetLogin(w, withLoginAlternatives(r, authServices[0].AuthType(), authServices))
		return
	}
	for _, provider := range authServices {
		if form, ok := provider.(LoginFormHandler); ok {
			form.GetLoginForm(w, withLoginAlternatives(r, provider.AuthType(), authServices))
			return
		}
	}
	var loginHTML []template.HTML
	for _, service := range authServices {
		loginHTML = append(loginHTML, service.GetLoginButton())
	}
	c := content.NewManagedContent(r, a.contentManager)
	c.PartialName = authLoginSelectionPartial
	c.Templates = a.templates[authLoginSelectionTemplate]
	c.Data = ProviderPage{Workflow: "login", Title: "Sign in", Controls: loginHTML}

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

	authService.GetLogin(w, a.prepareLoginRequest(r, authType))
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

	r = a.prepareLoginRequest(r, authType)
	u := authService.Login(w, r)
	if u == nil {
		return
	}
	a.completeLogin(w, r, u)
}

// Callback passes validated external credentials through the shared session flow.
func (a *AuthManager) Callback(w http.ResponseWriter, r *http.Request) {
	provider, ok := a.getAuthService(r.PathValue("authType")).(LoginCallbackHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}
	u := provider.Callback(w, r)
	if u != nil {
		a.completeLogin(w, r, u)
	}
}

// completeLogin establishes the same session and redirect for every provider.
func (a *AuthManager) completeLogin(w http.ResponseWriter, r *http.Request, u *user.User) {
	if !a.Policy.Admit(w, r, u.Email, a.contentManager) {
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
	if a.Policy.AfterLogin != nil {
		a.Policy.AfterLogin(w, r, u)
	}
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

type loginAlternativesKey struct{}

// LoginAlternatives returns other enabled providers' trusted login controls.
// The manager attaches them before rendering forms or processing submissions.
func LoginAlternatives(r *http.Request) []template.HTML {
	buttons, _ := r.Context().Value(loginAlternativesKey{}).([]template.HTML)
	return buttons
}

// loginChoices applies presentation policy without changing provider routes.
func (a *AuthManager) loginChoices(ctx context.Context) ([]LoginHandler, error) {
	var account *user.User
	if a.Policy.LoginAccount != nil {
		var err error
		account, err = a.Policy.LoginAccount(ctx)
		if err != nil {
			return nil, err
		}
	}
	var choices []LoginHandler
	for _, service := range a.Services.Services() {
		provider, ok := service.(LoginHandler)
		if !ok {
			continue
		}
		if availability, ok := service.(LoginAvailability); ok && account != nil {
			available, err := availability.LoginAvailable(ctx, account)
			if err != nil {
				// Unknown availability must not hide a usable login method or
				// prevent other providers from authenticating the account.
				if log := logger.Get(ctx); log != nil {
					log.ErrorContext(ctx, "login availability lookup failed", "provider", service.AuthType(), "error", err)
				}
				available = true
			}
			if !available {
				continue
			}
		}
		choices = append(choices, provider)
	}
	if account != nil && len(choices) == 0 {
		return nil, errors.New("no configured login provider supports the selected account")
	}
	return choices, nil
}

func (a *AuthManager) prepareLoginRequest(r *http.Request, selected string) *http.Request {
	choices, err := a.loginChoices(r.Context())
	if err != nil {
		logger.LogRequestError(r, err)
		// A direct provider route must not depend on optional presentation
		// lookups. Credential validation and admission still run normally.
		return withLoginAlternatives(r, selected, nil)
	}
	return withLoginAlternatives(r, selected, choices)
}

func withLoginAlternatives(r *http.Request, selected string, choices []LoginHandler) *http.Request {
	var buttons []template.HTML
	for _, provider := range choices {
		if provider.AuthType() != selected {
			buttons = append(buttons, provider.GetLoginButton())
		}
	}
	return r.WithContext(context.WithValue(r.Context(), loginAlternativesKey{}, buttons))
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
