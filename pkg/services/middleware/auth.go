// auth.go defines middleware related to authorization
package middleware

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jmoiron/sqlx"
	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// LoadAuthenticatedUser extracts the authenticated user and adds it to the context
func LoadAuthenticatedUser(db *sqlx.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, err := auth_services.GetAuthenticatedUser(r, db)

			var notFoundErr *user.ErrUserNotFound
			if err != nil && !errors.As(err, &notFoundErr) {
				// log the error encountered when trying to retrieve the user
				logger.LogRequestError(r, fmt.Errorf("unable to load the authenticated user: %w", err))
			}

			if u != nil {
				r = user.AddUserToRequestContext(r, u)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuthentication determines whether the user is authenticated and, if not, denies access
func RequireAuthentication(db *sqlx.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, err := getAuthenticatedUser(r, db)
			if err != nil {
				// don't allow access
				content.HandleError(w, r,
					"Unable to authenticate your account",
					err,
					http.StatusForbidden)
				return
			}

			if u == nil {
				// don't allow access
				content.HandleError(w, r,
					"You must be logged-in to access the requested resource",
					nil,
					http.StatusForbidden)
				return
			}

			if u.VerificationRequired && !u.Verified {
				// don't allow access
				content.HandleError(w, r,
					"Your account must be verified before accessing the requested resource",
					nil,
					http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnonymous determines whether the user is authenticated and, if so, denies access
func RequireAnonymous(db *sqlx.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, err := getAuthenticatedUser(r, db)
			if err != nil {
				// don't allow access
				content.HandleError(w, r,
					"Unable to authenticate your account",
					err,
					http.StatusForbidden)
				return
			}

			if u != nil {
				// don't allow access
				content.HandleError(w, r,
					"The requested resource is not available to authenticated users",
					nil,
					http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getAuthenticatedUser retrieves the current user from the request context and, if it's not there, it
// tries to retrieve the user from the session. If no user is found, nil is returned.
func getAuthenticatedUser(r *http.Request, db *sqlx.DB) (*user.User, error) {
	ctx := r.Context()

	u := user.GetUserFromContext(ctx)
	if u == nil {
		u, err := auth_services.GetAuthenticatedUser(r, db)
		if err != nil {
			var notFoundErr *user.ErrUserNotFound
			if !errors.As(err, &notFoundErr) {
				// log the error encountered when trying to retrieve the user
				logger.LogRequestError(r, fmt.Errorf("unable to load the authenticated user: %w", err))
			}
			return nil, err
		}

		return u, nil
	}

	return u, nil
}
