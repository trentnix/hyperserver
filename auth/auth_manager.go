// auth_manager.go defines the AuthManager struct and methods that will be used
// to handle general auth by calling specifically configured auth implementations
package auth

import (
	"errors"
	"html/template"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type (
	AuthManager struct {
		Enabled bool

		db *sqlx.DB

		VerificationTokenExpiration time.Duration
		ResetTokenExpiration        time.Duration

		ResetRequiresNewCredentials bool
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
	a.db = s.Database

	a.ResetTokenExpiration = s.Config.Auth.ResetTokenExpiration
	a.VerificationTokenExpiration = s.Config.Auth.VerificationTokenExpiration

	a.ResetRequiresNewCredentials = s.Config.Auth.ResetRequiresNewCredentials

	return nil
}

// Routes defines the routes the AuthManager handler will be responsible for
func (a *AuthManager) Routes(mux *http.ServeMux) {
	if a.Enabled {
		// login / logout
		mux.Handle("/auth/login", http.HandlerFunc(a.GetLogin))
		mux.Handle("GET /auth/login/{authType}", http.HandlerFunc(a.GetLoginService))
		mux.Handle("POST /auth/login/{authType}", http.HandlerFunc(a.Login))
		mux.Handle("/auth/logout", http.HandlerFunc(a.Logout))

		// register
		mux.Handle("/auth/register", http.HandlerFunc(a.GetRegister))
		mux.Handle("GET /auth/register/{authType}", http.HandlerFunc(a.GetRegisterService))
		mux.Handle("POST /auth/register/{authType}", http.HandlerFunc(a.Register))

		// reset
		mux.Handle("GET /auth/reset/request/{authType}", http.HandlerFunc(a.GetResetRequest))
		mux.Handle("POST /auth/reset/request/{authType}", http.HandlerFunc(a.ResetRequest))
		mux.Handle("GET /auth/reset/{authType}/{token}", http.HandlerFunc(a.GetReset))
		mux.Handle("POST /auth/reset/{authType}/{token}", http.HandlerFunc(a.Reset))
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
		content.HandleError(w, r, "could not render the login page", err, http.StatusInternalServerError)
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
		content.HandleError(w, r, "could not render the registration page", err, http.StatusInternalServerError)
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
		content.HandleError(w, r, "No authorization service was specified. Unable to login.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "No authorization service was found. Unable to login.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).GetLogin(w, r)
}

// Login starts the login process for the given AuthService (specified by authType)
func (a *AuthManager) Login(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		content.HandleError(w, r, "No authorization service was specified. Unable to login.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "No authorization service was found. Unable to login.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	hs_user := (*authService).Login(w, r)
	if hs_user == nil {
		return
	}

	setAuthenticatedUserErr := SetAuthenticatedUser(r, w, hs_user)
	if setAuthenticatedUserErr != nil {
		content.HandleError(w, r, "There was an internal error when trying to login", setAuthenticatedUserErr, http.StatusInternalServerError)
		return
	}

	r = user.AddUserToRequestContext(r, hs_user)
	err := messages.AddSuccessMessage(w, r, "You have been successfully logged in")
	if err != nil {
		logger.LogRequestError(r, err)
	}

	homeURL := content.HomeDefault
	contentManager := content.GetContentManager()
	if contentManager != nil {
		homeURL = contentManager.HomeURL
	}

	content.RedirectToURL(w, r, homeURL)
}

// Logout logs out any logged-in user
func (a *AuthManager) Logout(w http.ResponseWriter, r *http.Request) {
	logoutErr := LogoutAuthenticatedUser(r, w)
	if logoutErr != nil {
		content.HandleError(w, r, "There was an error trying to log out", logoutErr, http.StatusInternalServerError)
		return
	}

	clearedCtx := user.ClearUserFromContext(r.Context())
	r = r.WithContext(clearedCtx)

	err := messages.AddSuccessMessage(w, r, "You have been successfully logged out")
	if err != nil {
		logger.LogRequestError(r, err)
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
		content.HandleError(w, r, "No authorization service was specified. Unable to register.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "Register unavailable: the specified auth service was found.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).GetRegister(w, r)
}

// Register starts the registration process for the given AuthService (specified by authType)
func (a *AuthManager) Register(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		content.HandleError(w, r, "No authorization service was specified. Unable to register.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "Register unavailable: the specified auth service was found.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).Register(w, r)

	err := messages.AddSuccessMessage(w, r, "You have been successfully registered")
	if err != nil {
		logger.LogRequestError(r, err)
	}

	authURL := content.AuthDefault
	contentManager := content.GetContentManager()
	if contentManager != nil {
		authURL = contentManager.AuthURL
	}

	content.RedirectToURL(w, r, authURL)
}

// GetResetRequest renders the reset request page to the user using the specified AuthService
// implementation
func (a *AuthManager) GetResetRequest(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		content.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "No authorization service was found. Unable to reset authentication.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).GetResetRequest(w, r)
}

// Reset starts the authentication reset process for the specified AuthService implementation
func (a *AuthManager) ResetRequest(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		content.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "No authorization service was found. Unable to reset authentication.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).ResetRequest(w, r, a.ResetTokenExpiration)
}

// GetReset starts the authentication reset process for the specified AuthService implementation.
// The provided token must be validated before the reset form is displayed.
func (a *AuthManager) GetReset(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		content.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	token := r.PathValue("token")
	if token == "" {
		content.HandleError(w, r, "No reset token specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	var errTokenExpired user.ErrTokenExpired

	hs_user, err := user.ValidateResetToken(a.db, token)
	if err != nil {
		switch {
		case errors.As(err, &errTokenExpired):
			// show the reset request form with a message that the token is expired
			messages.AddErrorMessage(w, r, "Unable to reset the specified authorization: the reset request has expired")
			a.GetResetRequest(w, r)
			return
		default:
			content.HandleError(w, r, "Could not reset authorization: invalid token", nil, http.StatusInternalServerError)
			return
		}
	}

	if hs_user == nil {
		content.HandleError(w, r, "Could not reset authorization: invalid user", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "No authorization service was found. Unable to reset authentication.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).GetReset(w, r, token)
}

// Reset starts the authentication reset process for the specified AuthService implementation
func (a *AuthManager) Reset(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		content.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	token := r.PathValue("token")
	if token == "" {
		content.HandleError(w, r, "No reset token specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	var errTokenExpired user.ErrTokenExpired

	hs_user, err := user.ValidateResetToken(a.db, token)
	if err != nil {
		switch {
		case errors.As(err, &errTokenExpired):
			// show the reset request form with a message that the token is expired
			addMessageErr := messages.AddErrorMessage(w, r, "Unable to reset the specified authorization: the reset request has expired")
			if addMessageErr != nil {
				logger.LogRequestError(r, addMessageErr)
			}

			a.GetResetRequest(w, r)
			return
		default:
			content.HandleError(w, r, "Could not reset authorization: invalid token", nil, http.StatusInternalServerError)
			return
		}
	}

	if hs_user == nil {
		content.HandleError(w, r, "Could not reset authorization: invalid user", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		content.HandleError(w, r, "No authorization service was found. Unable to reset authentication.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	resetSuccessful := (*authService).Reset(w, r, hs_user, token, a.ResetRequiresNewCredentials)
	if resetSuccessful {
		// delete user reset token
		resetToken, tokenErr := user.GetAuthResetTokenByID(a.db, hs_user.ID)
		if tokenErr != nil {
			logger.LogRequestError(r, user.NewErrTokenNotFound(tokenErr))
		}

		tokenErr = resetToken.Delete(a.db)
		if tokenErr != nil {
			// couldn't delete the token - log it
			// this is a security risk as the token could be reused to change the password again
			logger.LogRequestError(r, database.NewErrDatabase(tokenErr))
		}

		// redirect the user to the auth page
		err := messages.AddSuccessMessage(w, r, "Your password has been updated. Login to access the site.")
		if err != nil {
			logger.LogRequestError(r, err)
		}

		authURL := content.AuthDefault
		contentManager := content.GetContentManager()
		if contentManager != nil {
			authURL = contentManager.AuthURL
		}

		content.RedirectToURL(w, r, authURL)
	}
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
