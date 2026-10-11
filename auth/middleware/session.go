package middleware

import (
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// LoadSessionManagement attaches the session manager, then loads the authenticated
// user into the request context. It requires non-nil account-reader and session dependencies.
func LoadSessionManagement(accounts user.AccountRepository, s *session.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return session.WithSessionManager(s)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var authUserErr error
			r, authUserErr = loadAuthenticatedUser(r, accounts)
			if authUserErr != nil {
				logger.LogRequestError(r, authUserErr)
				var invalidToken *session.ErrInvalidToken
				if errors.As(authUserErr, &invalidToken) {
					user.ClearAuthenticationCookie(w, r)
					util.HttpError(w, r, "Invalid session cookie", nil, http.StatusBadRequest)
					return
				}
				util.HttpError(w, r, "There was an error loading the authenticated user", nil, http.StatusInternalServerError)
				return
			}

			next.ServeHTTP(w, r)
		}))
	}
}

// loadAuthenticatedUser loads any authenticated user into the specified request's context
func loadAuthenticatedUser(r *http.Request, accounts user.AccountRepository) (*http.Request, error) {
	if accounts == nil {
		return r, database.NewErrDatabaseUnavailable(nil)
	}

	u, err := user.GetAuthenticatedUser(r, accounts)

	var notFoundErr *user.ErrUserNotFound
	if err != nil && !errors.As(err, &notFoundErr) {
		return r, err
	}

	if u != nil {
		return user.AddUserToRequestContext(r, u), nil
	}

	return r, nil
}
