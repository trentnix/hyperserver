package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const disconnectTemplate = "templates/html/pages/disconnect.html"

// disconnectAlternative requires an enabled password login before offering a
// disconnection. Other reauthentication methods can be added here without
// changing identity providers or making storage decide which logins are usable.
func (a *AuthManager) disconnectAlternative(ctx context.Context, u *user.User, provider AuthService) (PasswordLoginProvider, error) {
	identity, ok := provider.(ExternalIdentityProvider)
	if !ok || identity.IdentityIssuer() == "" {
		return nil, nil
	}
	if _, ok := a.accounts.(user.ExternalIdentityUnlinker); !ok {
		return nil, nil
	}
	for _, other := range a.Services.Services() {
		if other.AuthType() == provider.AuthType() {
			continue
		}
		passwordLogin, ok := other.(PasswordLoginProvider)
		if !ok {
			continue
		}
		available, err := passwordLogin.LoginAvailable(ctx, u)
		if err != nil {
			return nil, err
		}
		if available {
			return passwordLogin, nil
		}
	}
	return nil, nil
}

type disconnectForm struct {
	Name, Password string
	form.Form
}

func (f *disconnectForm) Bind(r *http.Request) error {
	f.Password = r.FormValue("password")
	return nil
}

func (f *disconnectForm) Validate() form.FieldErrors {
	if f.Password == "" {
		return form.FieldErrors{"Password": {form.RequiredMessage}}
	}
	return nil
}

// Disconnect confirms an alternative credential before removing a provider link.
func (a *AuthManager) Disconnect(w http.ResponseWriter, r *http.Request) {
	u := user.GetUserFromContext(r.Context())
	if u == nil || u.NeedsVerification() {
		a.contentManager.HandleError(w, r, "Sign in with a verified account to manage sign-in methods.", nil, http.StatusForbidden)
		return
	}
	if !a.Policy.Admit(w, r, u.Email, a.contentManager) {
		return
	}
	provider := a.getAuthService(r.PathValue("authType"))
	controls, ok := provider.(AccountProvider)
	if !ok {
		http.NotFound(w, r)
		return
	}
	alternative, err := a.disconnectAlternative(r.Context(), u, provider)
	if err != nil {
		a.contentManager.HandleError(w, r, "Account controls are temporarily unavailable.", err, http.StatusServiceUnavailable)
		return
	}
	if alternative == nil {
		a.contentManager.HandleError(w, r, "Add a password and enable password sign-in before disconnecting this method.", nil, http.StatusForbidden)
		return
	}
	method, err := controls.AccountMethod(r.Context(), u)
	if err != nil {
		a.contentManager.HandleError(w, r, "Account controls are temporarily unavailable.", err, http.StatusServiceUnavailable)
		return
	}
	if !method.Connected {
		util.RedirectToURL(w, r, "/auth/account")
		return
	}
	f := &disconnectForm{Name: method.Name}
	f.ActionUrl = "/auth/unlink/" + provider.AuthType()
	c := content.NewManagedContent(r, a.contentManager)
	c.Title, c.PartialName, c.Templates, c.Data = "Disconnect "+method.Name, "auth.page.disconnect", a.templates[disconnectTemplate], f
	if c.IsHtmx() && strings.HasPrefix(c.HTMX.Request.Target, "account-action-") {
		c.PartialName = "auth.page.disconnect.panel"
	}
	if r.Method == http.MethodPost {
		if err := form.ParseAndValidate(r, f); err != nil {
			form.HandleFormError(w, r, c, f, "Enter your password to disconnect this method.", err)
			return
		}
		if !alternative.CheckPassword(u, f.Password) {
			f.SetFieldErrors(form.FieldErrors{"Password": {"That password is incorrect."}})
			form.HandleFormError(w, r, c, f, "Your sign-in methods are unchanged.", nil)
			return
		}
		issuer := provider.(ExternalIdentityProvider).IdentityIssuer()
		if err := a.accounts.(user.ExternalIdentityUnlinker).UnlinkExternalIdentity(r.Context(), u, issuer); err != nil {
			logger.Audit(r.Context(), "identity_disconnect", "failed")
			if errors.Is(err, user.ErrUserChanged) {
				a.contentManager.HandleError(w, r, "Your account changed. Sign in again before disconnecting this method.", nil, http.StatusConflict)
			} else {
				a.contentManager.HandleError(w, r, "The sign-in method could not be disconnected. Please try again later.", err, http.StatusServiceUnavailable)
			}
			return
		}
		logger.Audit(r.Context(), "identity_disconnect", "succeeded")
		*r = *r.WithContext(user.ClearUserFromContext(r.Context()))
		identity.ClearAuthenticationCookie(w, r)
		if err := flash.AddSuccessNotification(w, r, method.Name+" is disconnected. Sign in with your password."); err != nil {
			logger.LogRequestError(r, errors.New("identity disconnected but notification could not be saved"))
		}
		util.RedirectToURL(w, r, a.contentManager.AuthURL)
		return
	}
	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "Unable to display account controls.", err)
	}
}
