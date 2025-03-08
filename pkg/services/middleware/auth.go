// auth.go defines middleware related to authorization
package middleware

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/jmoiron/sqlx"
	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/components/user"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// LoadAuthenticatedUser extracts the authenticated user and adds it to the context
func LoadAuthenticatedUser(db *sqlx.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			hs_user, err := auth_services.GetAuthenticatedUser(r, db)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				logger.LogRequestError(r, "unable to retrieve the authenticated user from the request", err)
			}

			if hs_user != nil {
				ctx = user.AddUserToContext(ctx, hs_user)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
