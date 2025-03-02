package auth

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
)

type (
	AuthManager struct{}
)

// init registers the AuthManager handler with the application
func init() {
	handlers.Register(new(AuthManager))
}

// Init processes the initialization of the AuthManager handler
func (a *AuthManager) Init(s *server.ApplicationServer) error {
	return nil
}

// Routes defines the routes the AuthManager handler will be responsible for
func (a *AuthManager) Routes(mux *http.ServeMux) {
	// login / logout
	// mux.Handle("/auth/login", http.HandlerFunc(a.GetLogin))
	mux.Handle("GET /auth/login/{authType}", http.HandlerFunc(a.GetLoginService))
}

// GetLoginService retrieves the first step of login process for the given AuthService
// (specified by authType)
func (a *AuthManager) GetLoginService(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	a.GetSpecificLoginService(w, r, authType)
}

// GetSpecificLoginService retrieves the specified AuthService and redirects the requestor to
// the specified service's login entry point
func (a *AuthManager) GetSpecificLoginService(w http.ResponseWriter, r *http.Request, authType string) {
	if authType == "" {
		http.Error(w, "No authorization service was specified. Unable to login.", http.StatusBadRequest)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		http.Error(w, "No authorization service was found. Unable to login.", http.StatusBadRequest)
		return
	}

	(*authService).GetLogin(w, r)
}

// getAuthService returns the authService specified by authType (if it is loaded)
func getAuthService(authType string) *AuthService {
	authServices := GetLoadedAuthServices()
	for _, service := range authServices {
		if authType == service.AuthType() && service.IsLoaded() {
			return &service
		}
	}

	return nil
}
