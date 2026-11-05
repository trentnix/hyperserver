package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const (
	emailSetupPath         = "/auth/password/setup/email"
	emailSetupCompletePath = "/auth/password/setup/email/complete"
	emailSetupFormTemplate = "templates/html/partials/password-setup.html"
	emailSetupFormPartial  = "auth.email.password.setup"
)

// Routes keeps password enrollment behind authentication. Redemption also
// requires the same account, so an emailed link cannot replace another identity.
func (a *EmailAuthService) Routes(routes, mutations *routing.Routes) error {
	protect := func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			authmiddleware.RequireAuthentication(a.accounts, a.contentManager)(h).ServeHTTP(w, r)
		})
	}
	routes.Handle("GET "+emailSetupPath, protect(a.passwordSetup))
	mutations.Handle("POST "+emailSetupPath, protect(a.passwordSetup))
	routes.Handle("GET "+emailSetupCompletePath, protect(a.completePasswordSetup))
	mutations.Handle("POST "+emailSetupCompletePath, protect(a.completePasswordSetup))
	return routes.Err()
}

func (a *EmailAuthService) setupAccount(w http.ResponseWriter, r *http.Request) *user.User {
	u := user.GetUserFromContext(r.Context())
	if u == nil || u.NeedsVerification() {
		a.contentManager.HandleError(w, r, "Sign in with a verified account to add a password.", nil, http.StatusForbidden)
		return nil
	}
	if !a.Policy.Admit(w, r, u.Email, a.contentManager) {
		return nil
	}
	if _, ok := a.accounts.(user.PasswordSetupRepository); !ok {
		a.contentManager.HandleError(w, r, "Password setup is not available.", nil, http.StatusServiceUnavailable)
		return nil
	}
	if u.Password != "" {
		util.RedirectToURL(w, r, "/auth/account")
		return nil
	}
	return u
}

func (a *EmailAuthService) passwordSetup(w http.ResponseWriter, r *http.Request) {
	u := a.setupAccount(w, r)
	if u == nil {
		return
	}
	data := struct {
		Email, Error string
		Sent         bool
	}{Email: u.Email}
	if r.Method == http.MethodPost {
		if err := form.Parse(r); err != nil {
			util.HttpError(w, r, err.Error(), nil, err.StatusCode)
			return
		}
		link, err := util.BuildPublicURL(r, a.config.HTTP, emailSetupCompletePath, nil)
		if err == nil {
			expiration := a.config.Auth.ResetTokenExpiration
			if expiration <= 0 {
				expiration = time.Hour
			}
			err = a.sendPasswordEmail(r.Context(), u, expiration, *link, true)
		}
		if err != nil {
			// Delivery errors may contain the bearer link. Do not echo or log them.
			data.Error = "We couldn't send the password setup email. Please try again."
		} else {
			data.Sent = true
		}
	}
	c := a.views[emailSetupFormPartial].Content(r, data)
	c.Title = "Add password"
	if c.IsHtmx() && strings.HasPrefix(c.HTMX.Request.Target, "account-action-") {
		c.PartialName = "auth.email.password.setup.panel"
	}
	if data.Error != "" {
		c.ResponseStatusCode = http.StatusServiceUnavailable
	}
	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "Unable to display password setup.", err)
	}
}

func (a *EmailAuthService) completePasswordSetup(w http.ResponseWriter, r *http.Request) {
	u := a.setupAccount(w, r)
	if u == nil {
		return
	}
	token := r.URL.Query().Get("token")
	owner, err := user.ValidatePasswordSetupToken(r.Context(), a.accounts, token)
	if err != nil || owner.ID != u.ID {
		if err != nil && !user.IsTokenRejection(err) {
			a.contentManager.HandleError(w, r, "Password setup is temporarily unavailable.", errors.New("password setup token lookup failed"), http.StatusServiceUnavailable)
		} else {
			a.contentManager.HandleError(w, r, "This password setup link is invalid or expired. Request another from Account.", nil, http.StatusBadRequest)
		}
		return
	}
	if r.Method == http.MethodPost {
		a.reset(w, r, u, token, false, true)
		return
	}
	a.getReset(w, r, token, true)
}
