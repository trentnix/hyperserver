package auth

import (
	"errors"
	"net/http"

	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/pkg/routing"
)

// PageHandlers supplies protected handlers without registering routes. Site
// modules register Login and Registration for GET /login and GET /register,
// and their Submit counterparts through Submissions for POST at the same paths.
// Provider forms and links use these established URLs.
type PageHandlers struct {
	Login, LoginSubmit               http.Handler
	Registration, RegistrationSubmit http.Handler
	Submissions                      *routing.Routes
}

// PageHandlers prepares handlers once during startup, after Init. It preserves
// anonymous-user checks, no-store responses, and independent submission limits.
func (a *AuthManager) PageHandlers(routes *routing.Routes) (PageHandlers, error) {
	if !a.Enabled || a.contentManager == nil {
		return PageHandlers{}, errors.New("auth pages require an initialized, enabled manager")
	}
	policy, err := a.mutationPolicy()
	if err != nil {
		return PageHandlers{}, err
	}
	submissions, err := routes.WithRateLimit(policy)
	if err != nil {
		return PageHandlers{}, err
	}
	protect := func(handler http.HandlerFunc) http.Handler {
		return noStore(authmiddleware.RequireAnonymous(a.accounts, a.contentManager)(handler))
	}
	pages := PageHandlers{
		Login: protect(a.GetLogin), LoginSubmit: protect(a.Login),
		Registration: protect(a.GetRegister), RegistrationSubmit: protect(a.Register),
		Submissions: submissions,
	}
	if !a.registrationEnabled {
		pages.Registration = noStore(http.HandlerFunc(RegistrationUnavailable))
		pages.RegistrationSubmit = pages.Registration
	}
	return pages, nil
}
