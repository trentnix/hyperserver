// auth_manager.go defines the AuthManager struct and methods that will be used
// to handle general auth by calling specifically configured auth implementations
package auth

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

type (
	AuthManager struct{}
)

const (
	authLoginSelectionTemplate = "auth/templates/html/login.html"
	authLoginDefaultTemplate   = "auth/templates/html/login-default.html"
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
	mux.Handle("/auth/login", http.HandlerFunc(a.GetLogin))
	mux.Handle("GET /auth/login/{authType}", http.HandlerFunc(a.GetLoginService))
	mux.Handle("POST /auth/login/{authType}", http.HandlerFunc(a.Login))
}

// Login handles serving the login page so the user can initiate a user login
func (a *AuthManager) GetLogin(w http.ResponseWriter, r *http.Request) {
	c := content.NewManagedContent(r)

	var loginHTML []template.HTML

	authServices := GetLoadedAuthServices()
	if len(authServices) == 1 {
		// there is only 1 auth service - display the default
		c.AddContentTemplate(authLoginDefaultTemplate)
		c.Data = "/auth/login/" + authServices[0].AuthType()
	} else {
		for _, service := range authServices {
			loginHTML = append(loginHTML, service.GetLoginButton())
		}

		c.AddContentTemplate(authLoginSelectionTemplate)
		c.Data = loginHTML
	}

	if err := c.Render(w, r); err != nil {
		renderingError := fmt.Sprintf("Could not render the home page: %s", err.Error())
		logger.LogRequestError(r, renderingError, err)
		content.RenderError(w, r, http.StatusInternalServerError, renderingError)
	}
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
		content.RenderError(w, r, http.StatusBadRequest, "No authorization service was specified. Unable to login.")
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.RenderError(w, r, http.StatusBadRequest, "No authorization service was found. Unable to login.")
		return
	}

	(*authService).GetLogin(w, r)
}

// Login starts the login process for the given AuthService (specified by authType)
func (a *AuthManager) Login(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		content.RenderError(w, r, http.StatusBadRequest, "Login unavailable: no authorization service was specified.")
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.RenderError(w, r, http.StatusBadRequest, "Login unavailable: the specified auth service was found.")
		return
	}

	(*authService).Login(w, r)
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
