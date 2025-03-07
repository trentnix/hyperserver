package auth

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
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
	mux.Handle("POST /auth/login/{authType}", http.HandlerFunc(a.Login))
}

// // Login handles serving the login page so the user can initiate a user login
// func (a *AuthManager) GetLogin(w http.ResponseWriter, r *http.Request) {
// 	p := page.NewPage(r)
// 	p.AddContents(authLoginSelectionTemplate)

// 	var loginHTML []template.HTML
// 	authServices := auth.GetLoadedAuthServices()
// 	for _, service := range authServices {
// 		loginHTML = append(loginHTML, service.GetLoginButton())
// 	}

// 	p.Data = loginHTML

// 	if err := a.RenderPage(w, p); err != nil {
// 		a.Site.ErrorDefault(w, err.Error(), http.StatusInternalServerError)
// 	}
// }

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
