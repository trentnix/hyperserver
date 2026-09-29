// auth_manager.go defines the AuthManager struct and methods that will be used
// to handle general auth by calling specifically configured auth implementations
package auth

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

type (
	AuthManager struct {
		Enabled bool

		db             *sqlx.DB
		contentManager *content_services.ContentManagerService

		verificationJwtKey   string
		resetTokenExpiration time.Duration

		resetRequiresNewCredentials  bool
		registerRequiresVerification bool
	}
)

const (
	authLoginSelectionTemplate    = "auth/templates/html/pages/login-selection.html"
	authRegisterSelectionTemplate = "auth/templates/html/pages/register-selection.html"
	authLoginDefaultTemplate      = "auth/templates/html/pages/login-default.html"
	authRegisterDefaultTemplate   = "auth/templates/html/pages/register-default.html"
	messageTemplate               = "auth/templates/html/pages/message.html"
	authLoginSelectionPartial     = "auth.page.login.selection"
	authRegisterSelectionPartial  = "auth.page.register.selection"
	authLoginDefaultPartial       = "auth.page.login.default"
	authRegisterDefaultPartial    = "auth.page.register.default"

	authEndpoint = "/auth/login"
)

// init registers the AuthManager handler with the application
func init() {
	handlers.Register(new(AuthManager))
}

// Init processes the initialization of the AuthManager handler
func (a *AuthManager) Init(s *server.ApplicationServer) error {
	a.Enabled = s.Config.Auth.Enabled
	a.db = s.Database

	if s.ContentManager == nil {
		return errors.New("Content Manager not configured")
	}

	a.contentManager = s.ContentManager

	a.verificationJwtKey = s.Config.Auth.JwtKey
	a.resetTokenExpiration = s.Config.Auth.ResetTokenExpiration

	a.resetRequiresNewCredentials = s.Config.Auth.ResetRequiresNewCredentials
	a.registerRequiresVerification = s.Config.Auth.RegisterRequiresVerification

	return nil
}

// Routes defines the routes the AuthManager handler will be responsible for
func (a *AuthManager) Routes(mux *http.ServeMux) {
	if a.Enabled {
		// login / logout
		mux.Handle(authEndpoint, middleware.RequireAnonymous(a.db, a.contentManager)(http.HandlerFunc(a.GetLogin)))
		mux.Handle("GET /auth/login/{authType}", middleware.RequireAnonymous(a.db, a.contentManager)(http.HandlerFunc(a.GetLoginService)))
		mux.Handle("POST /auth/login/{authType}", middleware.RequireAnonymous(a.db, a.contentManager)(http.HandlerFunc(a.Login)))
		mux.Handle("/auth/logout", http.HandlerFunc(a.Logout))

		// register
		mux.Handle("/auth/register", middleware.RequireAnonymous(a.db, a.contentManager)(http.HandlerFunc(a.GetRegister)))
		mux.Handle("GET /auth/register/{authType}", middleware.RequireAnonymous(a.db, a.contentManager)(http.HandlerFunc(a.GetRegisterService)))
		mux.Handle("POST /auth/register/{authType}", middleware.RequireAnonymous(a.db, a.contentManager)(http.HandlerFunc(a.Register)))

		// validate a registered user
		mux.Handle("GET /auth/verify", http.HandlerFunc(a.Verify))
		mux.Handle("POST /auth/request/verify", http.HandlerFunc(a.SendVerificationRequest))

		// reset credentials
		mux.Handle("GET /auth/reset/request/{authType}", middleware.RequireAnonymous(a.db, a.contentManager)(http.HandlerFunc(a.GetResetRequest)))
		mux.Handle("POST /auth/reset/request/{authType}", middleware.RequireAnonymous(a.db, a.contentManager)(http.HandlerFunc(a.ResetRequest)))
		mux.Handle("GET /auth/reset/{authType}", middleware.RequireAnonymous(a.db, a.contentManager)(http.HandlerFunc(a.GetReset)))
		mux.Handle("POST /auth/reset/{authType}", middleware.RequireAnonymous(a.db, a.contentManager)(http.HandlerFunc(a.Reset)))

		// change change credentials
		mux.Handle("GET /auth/change/{authType}", middleware.RequireAuthentication(a.db, a.contentManager)(http.HandlerFunc(a.GetChange)))
		mux.Handle("POST /auth/change/{authType}", middleware.RequireAuthentication(a.db, a.contentManager)(http.HandlerFunc(a.Change)))
	}
}

// GetLogin renders the various authentication options to a user trying to login
// to the application. If there is only a single authentication option, the user is
// sent directly to the login page of that authentication service.
func (a *AuthManager) GetLogin(w http.ResponseWriter, r *http.Request) {
	c := content.NewManagedContent(r, a.contentManager)

	var loginHTML []template.HTML

	authServices := GetLoadedAuthServices()
	if len(authServices) == 1 {
		// there is only 1 auth service - display the default
		c.PartialName = authLoginDefaultPartial
		c.AddContent(authLoginDefaultTemplate)
		c.Data = "/auth/login/" + authServices[0].AuthType()
	} else {
		c.PartialName = authLoginSelectionPartial
		for _, service := range authServices {
			loginHTML = append(loginHTML, service.GetLoginButton())
		}

		c.AddContent(authLoginSelectionTemplate)
		c.Data = loginHTML
	}

	if err := c.Render(w, r); err != nil {
		a.contentManager.HandleError(w, r, "could not render the login page", err, http.StatusInternalServerError)
	}
}

// GetRegister renders the various authentication options to a user trying to register
// with the application. If there is only a single authentication option, the user is
// sent directly to the registration page of that authentication service.
func (a *AuthManager) GetRegister(w http.ResponseWriter, r *http.Request) {
	c := content.NewManagedContent(r, a.contentManager)

	var loginHTML []template.HTML

	authServices := GetLoadedAuthServices()
	if len(authServices) == 1 {
		// there is only 1 auth service - display the default
		c.PartialName = authRegisterDefaultPartial
		c.AddContent(authRegisterDefaultTemplate)
		c.Data = "/auth/register/" + authServices[0].AuthType()
	} else {
		c.PartialName = authRegisterSelectionPartial
		for _, service := range authServices {
			loginHTML = append(loginHTML, service.GetRegisterButton())
		}

		c.AddContent(authRegisterSelectionTemplate)
		c.Data = loginHTML
	}

	if err := c.Render(w, r); err != nil {
		a.contentManager.HandleError(w, r, "could not render the registration page", err, http.StatusInternalServerError)
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
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to login.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		a.contentManager.HandleError(w, r, "No authorization service was found. Unable to login.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).GetLogin(w, r)
}

// Login starts the login process for the given AuthService (specified by authType)
func (a *AuthManager) Login(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to login.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		a.contentManager.HandleError(w, r, "No authorization service was found. Unable to login.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	u := (*authService).Login(w, r)
	if u == nil {
		return
	}

	setAuthenticatedUserErr := user.SetAuthenticatedUser(w, r, u)
	if setAuthenticatedUserErr != nil {
		a.contentManager.HandleError(w, r, "There was an internal error when trying to login", setAuthenticatedUserErr, http.StatusInternalServerError)
		return
	}

	r = user.AddUserToRequestContext(r, u)
	err := messages.AddSuccessNotification(w, r, "You have been successfully logged in")
	if err != nil {
		logger.LogRequestError(r, err)
	}

	// retrieve any session where a redirect URL might be stored
	// get a session for redirect values
	s, err := session.Get(r, session.AuthSession)
	if err != nil {
		// couldn't get a session, log the error
		logger.LogRequestError(r, err)
	}

	redirectURL := ""
	if !s.IsNew {
		msg, ok := s.Data[session.RedirectURL]
		if ok {
			sMessage, ok := msg.(string)
			if ok {
				redirectURL = sMessage
			}
		}
	}

	if redirectURL == "" {
		redirectURL = a.contentManager.HomeURL
	}

	// validate the URL
	uriErr := util.IsValidUri(redirectURL, false)
	if uriErr != nil {
		// redirect URL is invalid, log the error and use the content manager's home URL
		logger.LogRequestError(r, err)
	}

	// clear the redirect session, it is no longer valid
	s.End(w, r)

	util.RedirectToURL(w, r, redirectURL)
}

// Logout ends a session for any logged-in user
func (a *AuthManager) Logout(w http.ResponseWriter, r *http.Request) {
	logoutErr := user.LogoutAuthenticatedUser(w, r)
	if logoutErr != nil {
		a.contentManager.HandleError(w, r, "There was an error trying to log out", logoutErr, http.StatusInternalServerError)
		return
	}

	clearedCtx := user.ClearUserFromContext(r.Context())
	r = r.WithContext(clearedCtx)

	err := messages.AddSuccessNotification(w, r, "You have been successfully logged out")
	if err != nil {
		logger.LogRequestError(r, err)
	}

	util.RedirectToURL(w, r, a.contentManager.HomeURL)
}

// GetRegisterService retrieves the first step of registration process for the given
// AuthService (specified by authType)
func (a *AuthManager) GetRegisterService(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to register.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		a.contentManager.HandleError(w, r, "Register unavailable: the specified auth service was found.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).GetRegister(w, r)
}

// Register starts the registration process for the given AuthService (specified by authType)
func (a *AuthManager) Register(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to register.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		a.contentManager.HandleError(w, r, "Register unavailable: the specified auth service was found.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	registrationSuccessful := (*authService).Register(w, r)
	if !registrationSuccessful {
		return
	}

	successMessage := "You have been successfully registered"
	if a.registerRequiresVerification {
		successMessage = "You have been successfully registered. Check your email for verification instructions."
	}

	err := messages.AddSuccessNotification(w, r, successMessage)
	if err != nil {
		logger.LogRequestError(r, err)
	}

	util.RedirectToURL(w, r, a.contentManager.AuthURL)
}

// ProcessRegistration handles the registration process for the specified user
func ProcessRegistration(ctx context.Context, db *sqlx.DB, uname string, pw string, authType string, verificationRequired bool) (error, string) {
	if err := ctx.Err(); err != nil {
		return NewErrUserRegistration(uname, err), "There was an error creating a user account"
	}

	var err error
	if pw != "" {
		pw, err = password.HashPassword(pw)
		if err != nil {
			// the password could not be hashed
			registrationErr := NewErrUserRegistration(uname, err)
			return registrationErr, "There was an error creating a user account"
		}
	}

	u := &user.User{
		Email:                uname,
		Password:             pw,
		RegistrationAuthType: authType,
		VerificationRequired: verificationRequired,
	}

	// The unique email constraint decides which competing registration succeeds.
	err = u.Create(ctx, db)
	if err != nil {
		registrationErr := NewErrUserRegistration(uname, err)
		var duplicate *database.ErrRecordAlreadyExists
		if errors.As(err, &duplicate) {
			return registrationErr, "The specified user is already registered"
		}
		return registrationErr, "There was an error creating a user account"
	}

	return nil, ""
}

// GetResetRequest renders the reset request page to the user using the specified AuthService
// implementation
func (a *AuthManager) GetResetRequest(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		a.contentManager.HandleError(w, r, "No authorization service was found. Unable to reset authentication.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).GetResetRequest(w, r)
}

// Reset starts the authentication reset process for the specified AuthService implementation
func (a *AuthManager) ResetRequest(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		a.contentManager.HandleError(w, r, "No authorization service was found. Unable to reset authentication.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).ResetRequest(w, r, a.resetTokenExpiration)
}

// GetReset starts the authentication reset process for the specified AuthService implementation.
// The provided token must be validated before the reset form is displayed.
func (a *AuthManager) GetReset(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		a.contentManager.HandleError(w, r, "No reset token specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	var errTokenExpired user.ErrTokenExpired

	u, err := user.ValidateResetToken(a.db, token)
	if err != nil {
		switch {
		case errors.As(err, &errTokenExpired):
			// show the reset request form with a message that the token is expired
			messages.AddErrorNotification(w, r, "Unable to reset the specified authorization: the reset request has expired")
			a.GetResetRequest(w, r)
			return
		default:
			a.contentManager.HandleError(w, r, "Could not reset authorization: invalid token", nil, http.StatusInternalServerError)
			return
		}
	}

	if u == nil {
		a.contentManager.HandleError(w, r, "Could not reset authorization: invalid user", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		a.contentManager.HandleError(w, r, "No authorization service was found. Unable to reset authentication.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).GetReset(w, r, token)
}

// Reset starts the authentication reset process for the specified AuthService implementation
func (a *AuthManager) Reset(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		a.contentManager.HandleError(w, r, "No reset token specified. Unable to reset authentication.", nil, http.StatusInternalServerError)
		return
	}

	var errTokenExpired user.ErrTokenExpired

	u, err := user.ValidateResetToken(a.db, token)
	if err != nil {
		switch {
		case errors.As(err, &errTokenExpired):
			// show the reset request form with a message that the token is expired
			addMessageErr := messages.AddErrorNotification(w, r, "Unable to reset the specified authorization: the reset request has expired")
			if addMessageErr != nil {
				logger.LogRequestError(r, addMessageErr)
			}

			a.GetResetRequest(w, r)
			return
		default:
			a.contentManager.HandleError(w, r, "Could not reset authorization: invalid token", nil, http.StatusInternalServerError)
			return
		}
	}

	if u == nil {
		a.contentManager.HandleError(w, r, "Could not reset authorization: invalid user", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		a.contentManager.HandleError(w, r, "No authorization service was found. Unable to reset authentication.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	resetSuccessful := (*authService).Reset(w, r, u, token, a.resetRequiresNewCredentials)
	if resetSuccessful {
		// delete user reset token
		resetToken, tokenErr := user.GetAuthResetTokenByUser(a.db, u.ID)
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
		err := messages.AddSuccessNotification(w, r, "Your password has been updated. Login to access the site.")
		if err != nil {
			logger.LogRequestError(r, err)
		}

		util.RedirectToURL(w, r, a.contentManager.AuthURL)
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

// GetChange starts the authentication change process for the specified AuthService implementation.
// This should only be available to authenticated users.
func (a *AuthManager) GetChange(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to modify authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		a.contentManager.HandleError(w, r, "No authorization service was found. Unable to change authentication.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	(*authService).GetChange(w, r)
}

// Change directs an auth modification attempt to the correct AuthService instance
func (a *AuthManager) Change(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to modify authentication.", nil, http.StatusInternalServerError)
		return
	}

	authService := getAuthService(authType)
	if authService == nil {
		a.contentManager.HandleError(w, r, "No authorization service was found. Unable to change authentication.", NewErrAuthServiceNotFound(nil, authType), http.StatusInternalServerError)
		return
	}

	u := user.GetUserFromContext(r.Context())
	if u == nil {
		a.contentManager.HandleError(w, r, "You must be logged in to change your password.", nil, http.StatusInternalServerError)
		return
	}

	(*authService).Change(w, r, u)
}

// SendVerificationRequest processes a user verification request by sending verification
// instructions to the user
func (a *AuthManager) SendVerificationRequest(w http.ResponseWriter, r *http.Request) {
	authUser, err := user.GetAuthenticatedUser(r, a.db)
	if err != nil {
		logVerificationFailure(r, "verification account lookup failed")
		a.contentManager.HandleError(w, r, "Unable to send verification instructions. Please try again later.", nil, http.StatusInternalServerError)
		return
	}
	if authUser == nil {
		a.contentManager.HandleError(w, r, "Authentication required", nil, http.StatusUnauthorized)
		return
	}

	if authUser.NeedsVerification() {
		authService := getAuthService(authUser.RegistrationAuthType)
		var sender VerificationEmailSender
		if authService != nil {
			sender, _ = (*authService).(VerificationEmailSender)
		}
		if sender == nil {
			logVerificationFailure(r, "verification email provider is unavailable")
			a.contentManager.HandleError(w, r, "Unable to send verification instructions. Please try again later.", nil, http.StatusServiceUnavailable)
			return
		}
		origin, err := util.BuildUrl(r, a.contentManager.Host, a.contentManager.Port, "", nil)
		if err == nil {
			err = sender.SendVerificationEmail(r.Context(), authUser, *origin)
		}
		if err != nil {
			// Provider errors can include tokens or message bodies. Do not expose them.
			logVerificationFailure(r, "verification email delivery failed")
			a.contentManager.HandleError(w, r, "Unable to send verification instructions. Please try again later.", nil, http.StatusServiceUnavailable)
			return
		}
	}
	a.contentManager.HandleMessage(w, r, "If your account needs verification, check your email for instructions.")
}

func logVerificationFailure(r *http.Request, message string) {
	if logger.Get(r.Context()) != nil {
		logger.LogRequestError(r, errors.New(message))
	} else {
		log.Print(message)
	}
}

// Verify processes an attempt to verify a registered account
func (a *AuthManager) Verify(w http.ResponseWriter, r *http.Request) {
	verificationToken := r.URL.Query().Get("token")
	if verificationToken == "" {
		a.contentManager.HandleError(
			w,
			r,
			"No verification token specified. Unable to verify the user.",
			nil,
			http.StatusInternalServerError)
		return
	}

	verificationErrMsg := "Verification failed: unable to verify your user account."

	verifiedUser, err := user.Verify(r.Context(), a.db, verificationToken, []byte(a.verificationJwtKey))
	if err != nil {
		a.contentManager.HandleError(
			w,
			r,
			verificationErrMsg,
			fmt.Errorf("error retrieving the user from the verification token: %w", err),
			http.StatusBadRequest)
		return
	}

	if verifiedUser == nil {
		a.contentManager.HandleError(
			w,
			r,
			verificationErrMsg,
			fmt.Errorf("unable to retrieve the user from the verification token: %w", err),
			http.StatusBadRequest)
		return
	}

	authenticatedUser, _ := user.GetAuthenticatedUser(r, a.db)
	if authenticatedUser != nil {
		// a user is currently authenticated - redirect to Home with a custom message
		authenticatedUserMessage := "Your account was verified successfully."
		if authenticatedUser.Email != verifiedUser.Email {
			authenticatedUserMessage = "The account was verified successfully, but is not currently logged in. Logout and login with the newly verified account for access."
		}

		err = messages.AddSuccessNotification(w, r, authenticatedUserMessage)
		if err != nil {
			logger.LogRequestError(r, err)
		}

		util.RedirectToURL(w, r, a.contentManager.HomeURL)

		return
	}

	// no user is authenticated - redirect the requestor to the login page
	err = messages.AddSuccessNotification(w, r, "Your account was verified successfully. Please login to access the site.")
	if err != nil {
		logger.LogRequestError(r, err)
	}

	util.RedirectToURL(w, r, a.contentManager.HomeURL)
}
