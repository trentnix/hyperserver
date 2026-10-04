package middleware_test

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func ExampleRequireAuthorization() {
	// This policy allows users to view their own account after required verification.
	// Applications can instead query their own permission or ownership storage.
	canViewAccount := func(r *http.Request) (bool, error) {
		u := user.GetUserFromContext(r.Context())
		return u != nil && !u.NeedsVerification() && u.ID == r.PathValue("id"), nil
	}
	account := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux := http.NewServeMux()
	mux.Handle("GET /accounts/{id}", middleware.RequireAuthorization(canViewAccount)(account))
	// Wrap mux with identity-loading middleware before serving requests.
	// Register authorization on the route so PathValue is available to the policy.
}
