package middleware

import (
	"errors"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// LoadSessionManagement attaches the session manager, then loads the authenticated
// user into the request context. It requires non-nil database and session dependencies.
func LoadSessionManagement(db *sqlx.DB, s *session.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var sessionManagerErr error
			r, sessionManagerErr = loadSessionManager(r, s)
			if sessionManagerErr != nil {
				util.HttpError(w, r, "There was an error loading the session manager", sessionManagerErr, http.StatusInternalServerError)
				return
			}

			var authUserErr error
			r, authUserErr = loadAuthenticatedUser(r, db)
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
		})
	}
}

// loadSessionManager loads the specified Sessionmanager into the specified request's context
func loadSessionManager(r *http.Request, s *session.SessionManager) (*http.Request, error) {
	if s != nil {
		return session.AddSessionManagerToRequestContext(r, s), nil
	}

	return r, session.NewErrSessionManagerNotFound(nil)
}

// loadUser loads any authenticated user into the specified request's context
func loadAuthenticatedUser(r *http.Request, db *sqlx.DB) (*http.Request, error) {
	if db == nil {
		return r, database.NewErrDatabaseUnavailable(nil)
	}

	u, err := user.GetAuthenticatedUser(r, db)

	var notFoundErr *user.ErrUserNotFound
	if err != nil && !errors.As(err, &notFoundErr) {
		return r, err
	}

	if u != nil {
		return user.AddUserToRequestContext(r, u), nil
	}

	return r, nil
}
