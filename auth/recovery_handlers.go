package auth

import (
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// GetResetRequest renders the reset request page to the user using the specified AuthService
// implementation
func (a *AuthManager) GetResetRequest(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(ResetHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	authService.GetResetRequest(w, r)
}

// ResetRequest delegates a recovery request to the selected authentication provider.
// The provider writes the acknowledgment independently of the delivery result.
func (a *AuthManager) ResetRequest(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(ResetHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	authService.ResetRequest(w, r, a.resetTokenExpiration)
}

// GetReset starts the authentication reset process for the specified AuthService implementation.
// The provided token must be validated before the reset form is displayed.
func (a *AuthManager) GetReset(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(ResetHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	token, u := a.resetAccount(w, r)
	if u == nil {
		return
	}

	authService.GetReset(w, r, token)
}

// Reset validates the token before delegating redemption and the response to the provider.
func (a *AuthManager) Reset(w http.ResponseWriter, r *http.Request) {
	if err := form.Parse(r); err != nil {
		util.HttpError(w, r, err.Error(), nil, err.StatusCode)
		return
	}

	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(ResetHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	token, u := a.resetAccount(w, r)
	if u == nil {
		return
	}

	authService.Reset(w, r, u, token, a.resetRequiresNewCredentials)
}

// resetAccount applies the same token checks to the form and its submission.
func (a *AuthManager) resetAccount(w http.ResponseWriter, r *http.Request) (string, *user.User) {
	token := r.URL.Query().Get("token")
	u, err := user.ValidateResetToken(r.Context(), a.accounts, token, []byte(a.verificationJwtKey))
	if err != nil {
		a.recoveryError(w, r, "Unable to reset password", err)
		return "", nil
	}
	return token, u
}

func (a *AuthManager) recoveryError(w http.ResponseWriter, r *http.Request, message string, err error) {
	if user.IsTokenRejection(err) {
		a.contentManager.HandleError(w, r,
			message+": the link is invalid or expired. Please request a new link.", nil, http.StatusBadRequest)
		return
	}
	// Storage errors can contain tokens or account details. Log only the operation.
	a.contentManager.HandleError(w, r, message+". Please try again later.",
		errors.New(message+": recovery operation failed"), http.StatusInternalServerError)
}
