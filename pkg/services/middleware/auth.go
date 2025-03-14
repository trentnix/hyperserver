// auth.go defines middleware related to authorization
package middleware

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/jmoiron/sqlx"
	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// LoadAuthenticatedUser extracts the authenticated user and adds it to the context
func LoadAuthenticatedUser(db *sqlx.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hs_user, err := auth_services.GetAuthenticatedUser(r, db)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				logger.LogRequestError(r, fmt.Errorf("unable to retrieve the authenticated user from the request: %w", err))
			}

			if hs_user != nil {
				r = user.AddUserToRequestContext(r, hs_user)
			}

			next.ServeHTTP(w, r)
		})
	}
}
