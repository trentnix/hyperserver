// auth_manager.go defines the AuthManager struct and methods that will be used
// to handle general auth by calling specifically configured auth implementations
package auth

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/user"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

type (
	AuthManager struct {
		Enabled bool
	}
)

const (
	authLoginSelectionTemplate    = "auth/templates/html/login.html"
	authRegisterSelectionTemplate = "auth/templates/html/register.html"
	authLoginDefaultTemplate      = "auth/templates/html/login-default.html"
	authRegisterDefaultTemplate   = "auth/templates/html/register-default.html"
)

// init registers the AuthManager handler with the application
func init() {
	handlers.Register(new(AuthManager))
}

// Init processes the initialization of the AuthManager handler
func (a *AuthManager) Init(s *server.ApplicationServer) error {
	a.Enabled = s.Config.Auth.Enabled
	return nil
}

// Routes defines the routes the AuthManager handler will be responsible for
func (a *AuthManager) Routes(mux *http.ServeMux) {
	if a.Enabled {
		// login / logout
		mux.Handle("/auth/login", http.HandlerFunc(a.GetLogin))
		mux.Handle("GET /auth/login/{authType}", http.HandlerFunc(a.GetLoginService))
		mux.Handle("POST /auth/login/{authType}", http.HandlerFunc(a.Login))

		// register
		mux.Handle("/auth/register", http.HandlerFunc(a.GetRegister))
		mux.Handle("GET /auth/register/{authType}", http.HandlerFunc(a.GetRegisterService))
		mux.Handle("POST /auth/register/{authType}", http.HandlerFunc(a.Register))
	}
}

// GetLogin renders the various authentication options to a user trying to login
// to the application. If there is only a single authentication option, the user is
// sent directly to the login page of that authentication service.
func (a *AuthManager) GetLogin(w http.ResponseWriter, r *http.Request) {
	c := content.NewManagedContent(r)

	var loginHTML []template.HTML

	authServices := GetLoadedAuthServices()
	if len(authServices) == 1 {
		// there is only 1 auth service - display the default
		c.AddContent(authLoginDefaultTemplate)
		c.Data = "/auth/login/" + authServices[0].AuthType()
	} else {
		for _, service := range authServices {
			loginHTML = append(loginHTML, service.GetLoginButton())
		}

		c.AddContent(authLoginSelectionTemplate)
		c.Data = loginHTML
	}

	if err := c.Render(w, r); err != nil {
		content.HandleRenderingError(w, r, "could not render the login page", err)
	}
}

// GetRegister renders the various authentication options to a user trying to register
// with the application. If there is only a single authentication option, the user is
// sent directly to the registration page of that authentication service.
func (a *AuthManager) GetRegister(w http.ResponseWriter, r *http.Request) {
	c := content.NewManagedContent(r)

	var loginHTML []template.HTML

	authServices := GetLoadedAuthServices()
	if len(authServices) == 1 {
		// there is only 1 auth service - display the default
		c.AddContent(authRegisterDefaultTemplate)
		c.Data = "/auth/register/" + authServices[0].AuthType()
	} else {
		for _, service := range authServices {
			loginHTML = append(loginHTML, service.GetRegisterButton())
		}

		c.AddContent(authRegisterSelectionTemplate)
		c.Data = loginHTML
	}

	if err := c.Render(w, r); err != nil {
		content.HandleRenderingError(w, r, "could not render the registration page", err)
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
		content.HandleError(w, r, "No authorization service was specified. Unable to login.", nil)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "No authorization service was found. Unable to login.", nil)
		return
	}

	(*authService).GetLogin(w, r)
}

// Login starts the login process for the given AuthService (specified by authType)
func (a *AuthManager) Login(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		content.HandleError(w, r, "No authorization service was specified. Unable to login.", nil)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "No authorization service was found. Unable to login.", nil)
		return
	}

	hs_user := (*authService).Login(w, r)
	if hs_user == nil {
		return
	}

	setAuthenticatedUserErr := SetAuthenticatedUser(r, w, hs_user)
	if setAuthenticatedUserErr != nil {
		content.HandleError(w, r, "There was an internal error when trying to login", setAuthenticatedUserErr)
		return
	}

	r = user.AddUserToRequestContext(r, hs_user)
	err := content.AddSuccessMessage(w, r, "You have been successfully logged in")
	if err != nil {
		logger.LogRequestError(r, fmt.Errorf("Could not add the specified message: %w", err))
	}

	homeURL := content.HomeDefault
	contentManager := content.GetContentManager()
	if contentManager != nil {
		homeURL = contentManager.HomeURL
	}

	content.RedirectToURL(w, r, homeURL)
}

// GetRegisterService retrieves the first step of registration process for the given
// AuthService (specified by authType)
func (a *AuthManager) GetRegisterService(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		content.HandleError(w, r, "No authorization service was specified. Unable to register.", nil)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "Register unavailable: the specified auth service was found.", nil)
		return
	}

	(*authService).GetRegister(w, r)
}

// Register starts the registration process for the given AuthService (specified by authType)
func (a *AuthManager) Register(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		content.HandleError(w, r, "No authorization service was specified. Unable to register.", nil)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "Register unavailable: the specified auth service was found.", nil)
		return
	}

	(*authService).Register(w, r)

	authURL := content.AuthDefault
	contentManager := content.GetContentManager()
	if contentManager != nil {
		authURL = contentManager.AuthURL
	}

	content.RedirectToURL(w, r, authURL)
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
