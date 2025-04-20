// session.go contains middleware that loads the application's SessionManager into the request context and
// also loads any currently authenticated user into the request context
package middleware

import (
	"errors"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// LoadSessionManagement loads both the SessionManager instance from the server application and any
// currently authenticated user into the request context. A single middleware function was used
// instead of splitting these two steps because loading the user requires a session manager to be
// loaded. If the steps were split, the order the middleware functions to separately load the session manager
// and the authenticated user would have had to run in a specific order.
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
				util.HttpError(w, r, "There was an error loading the authenticated user", authUserErr, http.StatusInternalServerError)
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

	return nil, session.NewErrSessionManagerNotFound(nil)
}

// loadUser loads any authenticated user into the specified request's context
func loadAuthenticatedUser(r *http.Request, db *sqlx.DB) (*http.Request, error) {
	if db == nil {
		return nil, database.NewErrDatabaseUnavailable(nil)
	}

	u, err := user.GetAuthenticatedUser(r, db)

	var notFoundErr *user.ErrUserNotFound
	if err != nil && !errors.As(err, &notFoundErr) {
		return nil, err
	}

	if u != nil {
		return user.AddUserToRequestContext(r, u), nil
	}

	return r, nil
}
