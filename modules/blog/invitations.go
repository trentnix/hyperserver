package blog

import (
	"context"
	"net/http"

	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const invitationCookie = "blog_invitation"

type invitationPageKey struct{}

func clearInvitationCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: invitationCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: requestinfo.IsHTTPS(r), SameSite: http.SameSiteLaxMode})
}

// dismissInvitation discards this browser's link, not the user's invitation.
func (m *module) dismissInvitation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	clearInvitationCookie(w, r)
	util.RedirectToURL(w, r, loginURL)
}

// openInvitation removes the bearer token from the address bar before rendering
// forms. Visiting the link never verifies an account or consumes the token.
func (m *module) openInvitation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	raw := r.URL.Query().Get("token")
	account, err := user.ValidateInvitation(r.Context(), m.accounts, raw)
	if err != nil && !user.IsTokenRejection(err) {
		m.app.ContentManager.HandleError(w, r, "Unable to check your invitation. Please try again.", err, http.StatusServiceUnavailable)
		return
	}
	if err != nil || !account.Invited || account.Role == "" {
		m.app.ContentManager.HandleError(w, r, "This invitation is invalid or has expired. Ask an admin to send a new one.", nil, http.StatusBadRequest)
		return
	}
	current := user.GetUserFromContext(r.Context())
	if current != nil && current.ID != account.ID {
		m.app.ContentManager.HandleError(w, r, "Sign out before accepting an invitation for another account.", nil, http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: invitationCookie, Value: raw, Path: "/", HttpOnly: true,
		Secure: requestinfo.IsHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: int(m.settings.InviteExpiration.Seconds())})
	if current != nil {
		util.RedirectToURL(w, r, "/invite/accept")
		return
	}
	target := "/register"
	if account.RegistrationAuthType != "" {
		target = "/login"
	}
	util.RedirectToURL(w, r, target)
}

func (m *module) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	account := user.GetUserFromContext(r.Context())
	if account == nil {
		util.RedirectToURL(w, r, loginURL)
		return
	}
	token, err := user.RegistrationInvitation(r.Context(), m.accounts, account.Email)
	if err != nil && !user.IsTokenRejection(err) {
		m.app.ContentManager.HandleError(w, r, "Unable to check your invitation. Please try again.", err, http.StatusServiceUnavailable)
		return
	}
	if err != nil || token == nil || token.AccountID != account.ID {
		m.app.ContentManager.HandleError(w, r, "This invitation is invalid or has expired. Ask an admin to send a new one.", nil, http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodGet {
		m.renderUsers(w, r, "invitation", userPage{User: *account}, http.StatusOK)
		return
	}
	invitations, ok := m.accounts.(user.InvitationRepository)
	if !ok {
		m.app.ContentManager.HandleError(w, r, "Invitations are unavailable.", nil, http.StatusServiceUnavailable)
		return
	}
	if err := invitations.AcceptInvitation(r.Context(), account, token); err != nil {
		if user.IsTokenRejection(err) {
			m.app.ContentManager.HandleError(w, r, "Unable to accept this invitation. Please sign in again.", nil, http.StatusConflict)
		} else {
			m.app.ContentManager.HandleError(w, r, "Unable to accept your invitation. Please try again.", err, http.StatusServiceUnavailable)
		}
		return
	}
	clearInvitationCookie(w, r)
	if err := identity.SetAuthenticatedUser(w, r, account); err != nil {
		m.app.ContentManager.HandleError(w, r, "Your invitation was accepted. Sign in again to continue.", err, http.StatusServiceUnavailable)
		return
	}
	util.RedirectToURL(w, r, "/post/manage")
}

func (m *module) invitationContext(next http.Handler) http.Handler {
	if !m.settings.MultiUser {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if cookie, err := r.Cookie(invitationCookie); err == nil {
			account, err := user.ValidateInvitation(r.Context(), m.accounts, cookie.Value)
			if err != nil && !user.IsTokenRejection(err) {
				m.app.ContentManager.HandleError(w, r, "Unable to check your invitation. Please try again.", err, http.StatusServiceUnavailable)
				return
			}
			if err == nil && account.Invited && account.Role != "" {
				r = r.WithContext(user.WithInvitation(r.Context(), cookie.Value))
				r = r.WithContext(context.WithValue(r.Context(), invitationPageKey{}, true))
			} else {
				clearInvitationCookie(w, r)
			}
		}
		next.ServeHTTP(w, r)
	})
}
