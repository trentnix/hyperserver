package blog

import (
	"context"
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// returningUserCookie controls link visibility only. It never grants access.
const returningUserCookie = "blog_returning_user"

// AuthenticationPolicy supplies the blog's administrator and onboarding rules
// for application.Options.AuthPolicy. Configure the site before serving requests.
// The policy works with every configured provider, not just email and Google.
func (s *Site) AuthenticationPolicy() auth.Policy {
	return auth.Policy{
		LoginAccount:   s.loginAccount,
		AllowAccount:   s.access.AllowAccount,
		BeforeRegister: s.registrationAvailable,
		AfterLogin: func(w http.ResponseWriter, r *http.Request, _ *user.User) {
			http.SetCookie(w, &http.Cookie{
				Name: returningUserCookie, Value: "1", Path: "/", MaxAge: 365 * 24 * 60 * 60,
				HttpOnly: true, Secure: requestinfo.IsHTTPS(r), SameSite: http.SameSiteLaxMode,
			})
		},
	}
}

// loginAccount simplifies choices only when storage confirms a sole account.
// Other storage providers keep the full configured login choices.
func (s *Site) loginAccount(ctx context.Context) (*user.User, error) {
	if accounts, ok := s.access.setup.accounts.(user.SingleAccountReader); ok {
		return accounts.SingleAccount(ctx)
	}
	return nil, nil
}

// registrationAvailable stops offering registration after the administrator exists.
func (s *Site) registrationAvailable(w http.ResponseWriter, r *http.Request) bool {
	setup := s.access.setup
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
