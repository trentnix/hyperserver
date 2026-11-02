package auth

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// GetChange starts the authentication change process for the specified AuthService implementation.
// This should only be available to authenticated users.
func (a *AuthManager) GetChange(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to modify authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(ChangeHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	authService.GetChange(w, r)
}

// Change directs an auth modification attempt to the correct AuthService instance
func (a *AuthManager) Change(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to modify authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(ChangeHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	u := user.GetUserFromContext(r.Context())
	if u == nil {
		a.contentManager.HandleError(w, r, "You must be logged in to change your password.", nil, http.StatusInternalServerError)
		return
	}

	authService.Change(w, r, u)
}
