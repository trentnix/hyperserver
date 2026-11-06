package blog

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// returningUserCookie controls link visibility only. It never grants access.
const returningUserCookie = "blog_returning_user"

// AuthenticationPolicy supplies the blog's access and onboarding rules
// for application.Options.AuthPolicy. Configure the site before serving requests.
// The policy works with every configured provider, not just email and Google.
func (s *Site) AuthenticationPolicy() auth.Policy {
	return auth.Policy{
		LoginAccount:   s.loginAccount,
		AllowAccount:   s.access.AllowAccount,
		BeforeRegister: s.registrationAvailable,
		AfterLogin: func(w http.ResponseWriter, r *http.Request, _ *user.User) {
			if _, err := r.Cookie(invitationCookie); err == nil {
				clearInvitationCookie(w, r)
			}
			http.SetCookie(w, &http.Cookie{
				Name: returningUserCookie, Value: "1", Path: "/", MaxAge: 365 * 24 * 60 * 60,
				HttpOnly: true, Secure: requestinfo.IsHTTPS(r), SameSite: http.SameSiteLaxMode,
			})
		},
	}
}

// loginAccount considers site access, not retained accounts whose access was
// removed. Pending invitations keep all configured sign-in choices available.
func (s *Site) loginAccount(ctx context.Context) (*user.User, error) {
	users, err := s.access.administration.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	if !s.access.multiUser {
		users = slices.DeleteFunc(users, func(u user.User) bool { return u.RegistrationAuthType == "" })
	}
	if len(users) == 1 && !users[0].Invited {
		return &users[0], nil
	}
	if len(users) == 0 {
		// Before the first login, setup has not assigned the initial admin role.
		account, err := s.access.accounts.GetByEmail(ctx, s.access.setup.email)
		if errors.Is(err, user.ErrUserNotFound) {
			return nil, nil
		}
		return account, err
	}
	return nil, nil
}

// registrationAvailable opens registration for initial setup or pending invitations.
func (s *Site) registrationAvailable(w http.ResponseWriter, r *http.Request) bool {
	setup := s.access.setup
	if s.access.multiUser {
		users, err := s.access.administration.ListUsers(r.Context())
		if err != nil {
			s.content.HandleError(w, r, "Registration is temporarily unavailable.", err, http.StatusServiceUnavailable)
			return false
		}
		for _, account := range users {
			if account.Invited && account.RegistrationAuthType == "" {
				return true
			}
		}
	}
	_, err := setup.accounts.GetByEmail(r.Context(), setup.email)
	if errors.Is(err, user.ErrUserNotFound) {
		return true
	}
	if err != nil {
		s.content.HandleError(w, r, "Registration is temporarily unavailable. Please try again.", err, http.StatusServiceUnavailable)
		return false
	}
	util.RedirectToURL(w, r, loginURL)
	return false
}
