package google

import (
	"context"
	"net/http"

	"github.com/trentnix/hyperserver/auth"
	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// AccountMethod reports the connection and only actions that preserve a way in.
func (s *Service) AccountMethod(ctx context.Context, u *user.User) (auth.AccountMethod, error) {
	connected, err := s.LoginAvailable(ctx, u)
	method := auth.AccountMethod{Name: "Google", Connected: connected}
	if err != nil {
		return method, err
	}
	if connected {
		method.Description = "You can sign in with Google."
	} else {
		method.Description = "Connect Google as another way to sign in."
		method.Actions = []auth.AccountAction{{Label: "Connect Google", URL: "/auth/link/google"}}
	}
	return method, nil
}

// IdentityIssuer identifies the stored link managed by shared account controls.
func (*Service) IdentityIssuer() string { return issuer }

// Routes serves the local brand icon and binds account controls.
// State-changing routes use the shared authentication mutation limit.
func (s *Service) Routes(routes, mutations *routing.Routes) error {
	routes.HandleFunc("GET /auth/google/icon.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFileFS(w, r, assets, "static/google-g.png")
	})
	protect := func(next http.Handler) http.Handler {
		check := authmiddleware.RequireAuthentication(s.accounts, s.content)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			check.ServeHTTP(w, r)
		})
	}
	routes.Handle("GET /auth/link/google", protect(http.HandlerFunc(s.account)))
	mutations.Handle("POST /auth/link/google", protect(http.HandlerFunc(s.account)))
	return routes.Err()
}

// account shows the current connection and starts linking only when unconnected.
func (s *Service) account(w http.ResponseWriter, r *http.Request) {
	account := user.GetUserFromContext(r.Context())
	if account == nil || account.NeedsVerification() {
		s.fail(w, r, "Log in and verify your account before connecting Google.", nil, http.StatusForbidden)
		return
	}
	if !s.Policy.Admit(w, r, account.Email, s.content) {
		return
	}
	connected, err := s.identities.HasExternalIdentity(r.Context(), account.ID, issuer)
	if err != nil {
		s.fail(w, r, "Your Google connection is temporarily unavailable. Please try again.", err, http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodPost && !connected {
		s.start(w, r, "link")
		return
	}

	c := s.view.Content(r, struct{ Connected bool }{connected})
	c.Title = "Google login"
	c.PartialName = "auth.google.account"
	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "Unable to display your Google connection.", err)
	}
}
