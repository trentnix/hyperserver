package middleware

import (
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// RequireAuthorization enforces an application-defined policy on each request.
// The policy can inspect route values and identity already loaded into the request
// context. It must decide how to handle anonymous and unverified users. This
// middleware does not authenticate users or load sessions.
//
// A false decision returns 403. A nil policy or policy error returns 500 without
// calling the next handler. Errors are logged, not sent to the client, and must
// not contain secrets. Policies must support concurrent calls and use r.Context()
// for I/O. Responses use the same status for ordinary and HTMX requests.
func RequireAuthorization(policy func(*http.Request) (bool, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if policy == nil {
				logger.LogRequestError(r, errors.New("authorization policy not specified"))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			allowed, err := policy(r)
			if err != nil {
				logger.LogRequestError(r, err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if !allowed {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
