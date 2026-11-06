package auth

import (
	"context"
	"errors"
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const (
	authLoginSelectionTemplate = "templates/html/pages/login-selection.html"
	authLoginSelectionPartial  = "auth.page.login.selection"
	authEndpoint               = "/login"
)

// GetLogin renders one shared page using the available providers.
func (a *AuthManager) GetLogin(w http.ResponseWriter, r *http.Request) {
	choices, err := a.loginChoices(r.Context())
	if err != nil {
		a.contentManager.HandleError(w, r, "Login options are temporarily unavailable. Please try again.", err, http.StatusServiceUnavailable)
		return
	}
	if len(choices) == 0 {
		http.NotFound(w, r)
		return
	}
	selected := ""
	if values, present := r.URL.Query()["provider"]; present {
		if len(values) != 1 || values[0] == "" {
			util.HttpError(w, r, "Choose a sign-in method.", nil, http.StatusBadRequest)
			return
		}
		for _, provider := range choices {
			if _, inline := provider.(LoginFormHandler); inline && provider.AuthType() == values[0] {
				selected = provider.AuthType()
				break
			}
		}
		if selected == "" {
			http.NotFound(w, r)
			return
		}
	}
	a.renderLogin(w, r, choices, selected, "")
}

func (a *AuthManager) renderLogin(w http.ResponseWriter, r *http.Request, choices []LoginHandler, selected string, submitted template.HTML) {
	page := ProviderPage{Workflow: "login", Title: "Sign in", Form: submitted}
	if submitted == "" {
		for _, provider := range choices {
			if selected != "" && provider.AuthType() != selected {
				continue
			}
			if form, ok := provider.(LoginFormHandler); ok {
				var err error
				page.Form, err = form.LoginForm(r)
				if err != nil {
					a.contentManager.HandleError(w, r, "Unable to display the login form.", err, http.StatusInternalServerError)
					return
				}
				selected = provider.AuthType()
				break
			}
		}
	}
	for _, provider := range choices {
		if provider.AuthType() != selected {
			page.Controls = append(page.Controls, provider.GetLoginButton())
		}
	}
	a.renderAuthPage(w, r, page, authLoginSelectionTemplate, authLoginSelectionPartial)
}

// GetLoginService redirects supported legacy provider pages to the shared page.
func (a *AuthManager) GetLoginService(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.getAuthService(r.PathValue("authType")).(LoginHandler); !ok {
		http.NotFound(w, r)
		return
	}
	a.redirectLogin(w, r)
}

func (a *AuthManager) redirectLogin(w http.ResponseWriter, r *http.Request) {
	util.RedirectToURL(w, r, authEndpoint)
}

// Login dispatches a bounded form submission. Provider availability affects
// presentation, not authorization. Every provider still validates credentials.
func (a *AuthManager) Login(w http.ResponseWriter, r *http.Request) {
	if err := form.Parse(r); err != nil {
		util.HttpError(w, r, err.Error(), nil, err.StatusCode)
		return
	}
	values := r.PostForm["provider"]
	if len(values) != 1 || values[0] == "" {
		util.HttpError(w, r, "Choose a sign-in method.", nil, http.StatusBadRequest)
		return
	}
	selected := values[0]
	provider, ok := a.getAuthService(selected).(LoginHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}
	result := provider.Login(w, r)
	if result.Account != nil {
		a.completeLogin(w, r, result.Account)
	} else if result.Form != "" {
		choices, err := a.loginChoices(r.Context())
		if err != nil {
			logger.LogRequestError(r, err)
		}
		// Keep the submitted form even if optional availability lookups fail.
		a.renderLogin(w, r, choices, selected, result.Form)
	}
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

	if u.Invited {
		token, err := user.RegistrationInvitation(r.Context(), a.accounts, "")
		// A stale link or another recipient's invitation is not proof for this
		// account. Valid credentials still allow the normal verification flow.
		if user.IsTokenRejection(err) || token != nil && (token.AccountID != u.ID || user.EmailKey(token.Email) != user.EmailKey(u.Email)) {
			token, err = nil, nil
		}
		if err != nil {
			a.contentManager.HandleError(w, r, "Unable to check your invitation. Please try again.", err, http.StatusServiceUnavailable)
			return
		}
		if !u.NeedsVerification() || token != nil {
			invitations, ok := a.accounts.(user.InvitationRepository)
			if !ok {
				a.contentManager.HandleError(w, r, "Account invitations are unavailable.", nil, http.StatusServiceUnavailable)
				return
			}
			if err := invitations.AcceptInvitation(r.Context(), u, token); err != nil {
				a.contentManager.HandleError(w, r, "Unable to accept this invitation. Try signing in again.", err, http.StatusConflict)
				return
			}
		}
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
