package session

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/util"
)

// WithSessionManager attaches the application's session manager without loading
// an identity or requiring account storage. A missing manager returns HTTP 500.
func WithSessionManager(s *SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s == nil {
				util.HttpError(w, r, "There was an error loading the session manager", ErrSessionManagerNotFound, http.StatusInternalServerError)
				return
			}

			next.ServeHTTP(w, AddSessionManagerToRequestContext(r, s))
		})
	}
}
