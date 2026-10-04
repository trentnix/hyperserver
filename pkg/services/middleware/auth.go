package middleware

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/database"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// RequireAuthentication determines whether the user is authenticated and, if not, denies access
func RequireAuthentication(db *sqlx.DB, cm *content_services.ContentManagerService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if db == nil {
				util.HttpError(w, r, "database not available", database.NewErrDatabaseUnavailable(nil), http.StatusInternalServerError)
				return
			}

			if cm == nil {
				util.HttpError(w, r, "content manager not specified", errors.New("content manager not specified"), http.StatusInternalServerError)
				return
			}

			u, err := getAuthenticatedUser(r, db)
			if err != nil {
				// don't allow access
				cm.HandleError(w, r,
					"Unable to authenticate your account",
					err,
					http.StatusForbidden)
				return
			}

			if u == nil {
				// create a session for redirect values
				s, err := session.New(r, session.AuthSession)
				if err != nil {
					// couldn't do a redirect so just don't allow access
					cm.HandleError(w, r,
						"The requested resource is only available to authenticated users",
						err,
						http.StatusForbidden)
					return
				}

				redirectURL := r.RequestURI
				s.Data[session.RedirectURL] = redirectURL
				sessionErr := s.Save(w, r)
				if sessionErr != nil {
					logger.LogRequestError(r, sessionErr)
					util.HttpError(w, r, "Unable to save the login session", nil, http.StatusInternalServerError)
					return
				}

				if err := messages.AddMessage(w, r, "The requested resource is only available to authenticated users.", messages.AuthMessages); err != nil {
					logger.LogRequestError(r, err)
				}

				// redirect to login
				util.RedirectToURL(w, r, cm.AuthURL)

				return
			}

			if u.NeedsVerification() {
				// don't allow access
				cm.HandleError(w, r,
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
func RequireAnonymous(db *sqlx.DB, cm *content_services.ContentManagerService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if db == nil {
				util.HttpError(w, r, "database not available", database.NewErrDatabaseUnavailable(nil), http.StatusInternalServerError)
				return
			}

			if cm == nil {
				util.HttpError(w, r, "content manager not specified", errors.New("content manager not specified"), http.StatusInternalServerError)
				return
			}

			u, err := getAuthenticatedUser(r, db)
			if err != nil {
				// don't allow access
				cm.HandleError(w, r,
					"Unable to authenticate your account",
					err,
					http.StatusForbidden)
				return
			}

			if u != nil {
				// don't allow access
				cm.HandleError(w, r,
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
		u, err := user.GetAuthenticatedUser(r, db)
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
