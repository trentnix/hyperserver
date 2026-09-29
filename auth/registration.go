package auth

import "net/http"

// RegistrationUnavailable rejects registration requests when registration is disabled.
func RegistrationUnavailable(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Registration is not available.", http.StatusForbidden)
}
