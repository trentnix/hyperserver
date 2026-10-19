// Package auth coordinates authentication providers and their HTTP handlers.
// Go package imports register factories. The auth module creates and initializes
// each application's manager and enabled providers before binding their routes.
package auth

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/trentnix/hyperserver/auth/identity"
	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// AuthManager routes authentication requests to registered providers.
// Init must run before Routes. When authentication is enabled, supply Services
// before Init and initialize its providers before serving requests.
type (
	AuthManager struct {
		Enabled  bool
		Services *Registry

		accounts       user.AccountRepository
		sessions       *session.SessionManager
		httpConfig     config.HTTPConfig
		rateLimit      *config.ModuleRateLimitConfig
		contentManager *content_services.ContentManagerService
		templates      map[string]*content.Templates

		verificationJwtKey   string
		resetTokenExpiration time.Duration

		resetRequiresNewCredentials bool
		registrationEnabled         bool
	}
)

const (
	authLoginSelectionTemplate    = "templates/html/pages/login-selection.html"
	authRegisterSelectionTemplate = "templates/html/pages/register-selection.html"
	authLoginDefaultTemplate      = "templates/html/pages/login-default.html"
	authRegisterDefaultTemplate   = "templates/html/pages/register-default.html"
	authVerifyTemplate            = "templates/html/pages/verify.html"
	messageTemplate               = "templates/html/pages/message.html"
	authLoginSelectionPartial     = "auth.page.login.selection"
	authRegisterSelectionPartial  = "auth.page.register.selection"
	authLoginDefaultPartial       = "auth.page.login.default"
	authRegisterDefaultPartial    = "auth.page.register.default"

	authEndpoint = "/auth/login"
)

// NewAuthManager supplies accounts and sessions for authentication handlers.
// The caller owns both services and must prepare them before Init. When authentication
// is enabled, set Services before initialization. Construction performs no I/O.
func NewAuthManager(accounts user.AccountRepository, sessions *session.SessionManager) *AuthManager {
	return &AuthManager{accounts: accounts, sessions: sessions}
}

// Init validates dependencies and configures authentication handlers.
func (a *AuthManager) Init(_ context.Context, s *server.ApplicationServer) error {
	if s.Config.Auth.Enabled && a.Services == nil {
		return errors.New("authentication requires an application-owned provider registry")
	}
	if s.Config.Auth.Enabled {
		if a.accounts == nil {
			return errors.New("authentication requires an account repository")
		}
		store, err := a.sessions.Store("auth-user-session")
		if err != nil {
			return fmt.Errorf("authentication requires session revocation: %w", err)
		}
		if _, ok := store.(session.RotatingStore); !ok {
			return errors.New("authentication requires session revocation: selected store does not support revocable session rotation")
		}
		if _, err := a.sessions.Store(session.AuthSession); err != nil {
			return fmt.Errorf("authentication requires a redirect session: %w", err)
		}
	}

	a.Enabled = s.Config.Auth.Enabled
	a.httpConfig = s.Config.HTTP
	a.rateLimit = s.Config.Auth.RateLimit

	if s.ContentManager == nil {
		return errors.New("Content Manager not configured")
	}

	a.contentManager = s.ContentManager

	a.verificationJwtKey = s.Config.Auth.JwtKey
	a.resetTokenExpiration = s.Config.Auth.ResetTokenExpiration

	a.resetRequiresNewCredentials = s.Config.Auth.ResetRequiresNewCredentials
	a.registrationEnabled = s.Config.Auth.Enabled && s.Config.Auth.RegistrationEnabled

	return a.prepareTemplates()
}

func (a *AuthManager) prepareTemplates() error {
	prepared := make(map[string]*content.Templates)
	for _, name := range []string{
		authLoginDefaultTemplate,
		authLoginSelectionTemplate,
		authRegisterDefaultTemplate,
		authRegisterSelectionTemplate,
		authVerifyTemplate,
		"templates/html/pages/verification-request.html",
	} {
		templates, err := content.PrepareTemplates(a.contentManager, content.TemplatePath{FS: authModuleTemplatesFS, Path: name})
		if err != nil {
			return fmt.Errorf("prepare auth template %s: %w", name, err)
		}
		prepared[name] = templates
	}
	a.templates = prepared
	return nil
}

// Routes defines the routes the AuthManager handler will be responsible for
func (a *AuthManager) Routes(mux *routing.Routes) error {
	if a.Enabled {
		maxClients, err := a.rateLimit.ClientCapacity()
		if err != nil {
			return fmt.Errorf("auth.rateLimit.%w", err)
		}
		anonymous := func(handler http.Handler) http.Handler {
			return noStore(authmiddleware.RequireAnonymous(a.accounts, a.contentManager)(handler))
		}
		authenticated := func(handler http.Handler) http.Handler {
			return noStore(authmiddleware.RequireAuthentication(a.accounts, a.contentManager)(handler))
		}
		mutations, err := mux.WithRateLimit(ratelimit.Policy{Requests: 20, Window: time.Minute, MaxClients: maxClients})
		if err != nil {
			return err
		}

		// login / logout
		mux.Handle("GET "+authEndpoint, anonymous(http.HandlerFunc(a.GetLogin)))
		mux.Handle("GET /auth/login/{authType}", anonymous(http.HandlerFunc(a.GetLoginService)))
		mutations.Handle("POST /auth/login/{authType}", anonymous(http.HandlerFunc(a.Login)))
		mux.WithoutRateLimit().Handle("POST /auth/logout", noStore(http.HandlerFunc(a.Logout)))

		// register
		if a.registrationEnabled {
			mux.Handle("GET /auth/register", anonymous(http.HandlerFunc(a.GetRegister)))
			mux.Handle("GET /auth/register/{authType}", anonymous(http.HandlerFunc(a.GetRegisterService)))
			mutations.Handle("POST /auth/register/{authType}", anonymous(http.HandlerFunc(a.Register)))
		} else {
			mux.Handle("/auth/register", noStore(http.HandlerFunc(RegistrationUnavailable)))
			mux.Handle("/auth/register/{authType}", noStore(http.HandlerFunc(RegistrationUnavailable)))
		}

		// validate a registered user
		mux.Handle("GET /auth/verify", noStore(http.HandlerFunc(a.GetVerify)))
		mux.Handle("GET /auth/request/verify", noStore(http.HandlerFunc(a.GetVerificationRequest)))
		mutations.Handle("POST /auth/verify", noStore(http.HandlerFunc(a.Verify)))
		mutations.Handle("POST /auth/request/verify", noStore(http.HandlerFunc(a.SendVerificationRequest)))

		// reset credentials
		mux.Handle("GET /auth/reset/request/{authType}", anonymous(http.HandlerFunc(a.GetResetRequest)))
		mutations.Handle("POST /auth/reset/request/{authType}", anonymous(http.HandlerFunc(a.ResetRequest)))
		mux.Handle("GET /auth/reset/{authType}", anonymous(http.HandlerFunc(a.GetReset)))
		mutations.Handle("POST /auth/reset/{authType}", anonymous(http.HandlerFunc(a.Reset)))

		// change change credentials
		mux.Handle("GET /auth/change/{authType}", authenticated(http.HandlerFunc(a.GetChange)))
		mutations.Handle("POST /auth/change/{authType}", authenticated(http.HandlerFunc(a.Change)))
	}
	return mux.Err()
}

// noStore applies the auth response policy before handlers or access checks write
// a form, redirect, or error. Other modules choose their own cache policies.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// GetLogin renders the various authentication options to a user trying to login
// to the application. If there is only a single authentication option, the user is
// sent directly to the login page of that authentication service.
func (a *AuthManager) GetLogin(w http.ResponseWriter, r *http.Request) {
	c := content.NewManagedContent(r, a.contentManager)

	var loginHTML []template.HTML

	var authServices []LoginHandler
	for _, service := range a.Services.Services() {
		if handler, ok := service.(LoginHandler); ok {
			authServices = append(authServices, handler)
		}
	}
	if len(authServices) == 0 {
		http.NotFound(w, r)
		return
	}
	if len(authServices) == 1 {
		// there is only 1 auth service - display the default
		c.PartialName = authLoginDefaultPartial
		c.Templates = a.templates[authLoginDefaultTemplate]
		c.Data = "/auth/login/" + authServices[0].AuthType()
	} else {
		c.PartialName = authLoginSelectionPartial
		for _, service := range authServices {
			loginHTML = append(loginHTML, service.GetLoginButton())
		}

		c.Templates = a.templates[authLoginSelectionTemplate]
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
	if !a.registrationEnabled {
		RegistrationUnavailable(w, r)
		return
	}

	c := content.NewManagedContent(r, a.contentManager)

	var loginHTML []template.HTML

	var authServices []RegistrationHandler
	for _, service := range a.Services.Services() {
		if handler, ok := service.(RegistrationHandler); ok {
			authServices = append(authServices, handler)
		}
	}
	if len(authServices) == 0 {
		RegistrationUnavailable(w, r)
		return
	}
	if len(authServices) == 1 {
		// there is only 1 auth service - display the default
		c.PartialName = authRegisterDefaultPartial
		c.Templates = a.templates[authRegisterDefaultTemplate]
		c.Data = "/auth/register/" + authServices[0].AuthType()
	} else {
		c.PartialName = authRegisterSelectionPartial
		for _, service := range authServices {
			loginHTML = append(loginHTML, service.GetRegisterButton())
		}

		c.Templates = a.templates[authRegisterSelectionTemplate]
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

	authService, ok := a.getAuthService(authType).(LoginHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	authService.GetLogin(w, r)
}

// Login starts the login process for the given AuthService (specified by authType)
func (a *AuthManager) Login(w http.ResponseWriter, r *http.Request) {
	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to login.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(LoginHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	u := authService.Login(w, r)
	if u == nil {
		logger.Audit(r.Context(), "login", "rejected")
		return
	}

	// Load the redirect session before issuing an authenticated session.
	s, err := session.Get(r, session.AuthSession)
	if err != nil {
		logger.LogRequestError(r, err)
		if errors.Is(err, session.ErrInvalidToken) {
			session.ExpireCookie(w, r, session.AuthSession)
			util.HttpError(w, r, "Invalid session cookie", nil, http.StatusBadRequest)
			return
		}
		util.HttpError(w, r, "Unable to load the login session", nil, http.StatusInternalServerError)
		return
	}

	setAuthenticatedUserErr := identity.SetAuthenticatedUser(w, r, u)
	if setAuthenticatedUserErr != nil {
		logger.Audit(r.Context(), "login", "failed")
		logger.LogRequestError(r, setAuthenticatedUserErr)
		util.HttpError(w, r, "There was an internal error when trying to login", nil, http.StatusInternalServerError)
		return
	}

	logger.Audit(r.Context(), "login", "succeeded")
	r = user.AddUserToRequestContext(r, u)
	err = flash.AddSuccessNotification(w, r, "You have been successfully logged in")
	if err != nil {
		logger.LogRequestError(r, errors.New("login succeeded but its notification could not be saved"))
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

	redirectURL = util.SafeRedirectURL(redirectURL, a.contentManager.HomeURL)
	if u.NeedsVerification() {
		redirectURL = "/auth/request/verify"
	}

	// clear the redirect session, it is no longer valid
	if err := s.End(w, r); err != nil {
		logger.LogRequestError(r, errors.New("login succeeded but the redirect session could not be cleared"))
	}

	util.RedirectToURL(w, r, redirectURL)
}

// Logout ends a session for any logged-in user
func (a *AuthManager) Logout(w http.ResponseWriter, r *http.Request) {
	logoutErr := identity.LogoutAuthenticatedUser(w, r)
	if logoutErr != nil {
		logger.Audit(r.Context(), "logout", "failed")
		logger.LogRequestError(r, logoutErr)
		util.HttpError(w, r, "There was an error trying to log out", nil, http.StatusInternalServerError)
		return
	}

	logger.Audit(r.Context(), "logout", "succeeded")
	err := flash.AddSuccessNotification(w, r, "You have been successfully logged out")
	if err != nil {
		logger.LogRequestError(r, err)
	}

	util.RedirectToURL(w, r, a.contentManager.HomeURL)
}

// GetRegisterService retrieves the first step of registration process for the given
// AuthService (specified by authType)
func (a *AuthManager) GetRegisterService(w http.ResponseWriter, r *http.Request) {
	if !a.registrationEnabled {
		RegistrationUnavailable(w, r)
		return
	}

	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to register.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(RegistrationHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	authService.GetRegister(w, r)
}

// Register starts the registration process for the given AuthService (specified by authType)
func (a *AuthManager) Register(w http.ResponseWriter, r *http.Request) {
	if !a.registrationEnabled {
		RegistrationUnavailable(w, r)
		return
	}

	authType := r.PathValue("authType")
	if authType == "" {
		a.contentManager.HandleError(w, r, "No authorization service was specified. Unable to register.", nil, http.StatusInternalServerError)
		return
	}

	authService, ok := a.getAuthService(authType).(RegistrationHandler)
	if !ok {
		http.NotFound(w, r)
		return
	}

	authService.Register(w, r)
}

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

// getAuthService finds a configured provider by its authType.
func (a *AuthManager) getAuthService(authType string) AuthService {
	authServices := a.Services.Services()
	for _, service := range authServices {
		if authType == service.AuthType() {
			return service
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

// SendVerificationRequest processes a user verification request by sending verification
// instructions to the user
func (a *AuthManager) SendVerificationRequest(w http.ResponseWriter, r *http.Request) {
	authUser, err := identity.GetAuthenticatedUser(r, a.accounts)
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
		authService := a.getAuthService(authUser.RegistrationAuthType)
		var sender VerificationEmailSender
		if authService != nil {
			sender, _ = authService.(VerificationEmailSender)
		}
		if sender == nil {
			logVerificationFailure(r, "verification email provider is unavailable")
			a.renderVerificationRequest(w, r, true, "Unable to send verification instructions. Please try again later.", http.StatusServiceUnavailable)
			return
		}

		origin, err := util.BuildPublicURL(r, a.httpConfig, "", nil)
		if err == nil {
			err = sender.SendVerificationEmail(r.Context(), authUser, *origin)
		}
		if err != nil {
			// Provider errors can include tokens or message bodies. Do not expose them.
			logVerificationFailure(r, "verification email delivery failed")
			a.renderVerificationRequest(w, r, true, "Unable to send verification instructions. Please try again later.", http.StatusServiceUnavailable)
			return
		}
	}

	a.renderVerificationRequest(w, r, authUser.NeedsVerification(), "Verification email sent. Please check your email for instructions.", http.StatusOK)
}

func logVerificationFailure(r *http.Request, message string) {
	if logger.Get(r.Context()) != nil {
		logger.LogRequestError(r, errors.New(message))
	} else {
		log.Print(message)
	}
}

// GetVerify displays a confirmation form without consuming the verification token.
func (a *AuthManager) GetVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	verificationToken := r.URL.Query().Get("token")
	if verificationToken == "" {
		a.contentManager.HandleError(w, r, "No verification token specified. Unable to verify the user.", nil, http.StatusBadRequest)
		return
	}

	c := content.NewManagedContent(r, a.contentManager)
	c.PartialName = "auth.page.verify"
	c.Templates = a.templates[authVerifyTemplate]
	c.Data = verificationToken
	if err := c.Render(w, r); err != nil {
		a.contentManager.HandleError(w, r, "Unable to display the verification form.", err, http.StatusInternalServerError)
	}
}

// Verify consumes the submitted token and verifies the account's email address.
func (a *AuthManager) Verify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := form.Parse(r); err != nil {
		util.HttpError(w, r, err.Error(), nil, err.StatusCode)
		return
	}
	verificationToken := r.PostForm.Get("token")
	if verificationToken == "" {
		a.contentManager.HandleError(w, r, "No verification token specified. Unable to verify the user.", nil, http.StatusBadRequest)
		return
	}

	verificationErrMsg := "Verification failed: unable to verify your user account."

	authenticatedUser, err := identity.GetAuthenticatedUser(r, a.accounts)
	if err != nil {
		logVerificationFailure(r, "verification account lookup failed")
		util.HttpError(w, r, "Unable to verify your account. Please try again later.", nil, http.StatusInternalServerError)
		return
	}

	verifiedUser, err := user.Verify(r.Context(), a.accounts, verificationToken, []byte(a.verificationJwtKey))
	if err != nil {
		logger.Audit(r.Context(), "verification", "failed")
	} else {
		logger.Audit(r.Context(), "verification", "succeeded")
	}
	if err != nil {
		a.recoveryError(w, r, "Verification failed", err)
		return
	}

	if verifiedUser == nil {
		a.contentManager.HandleError(
			w,
			r,
			verificationErrMsg,
			errors.New("verification repository returned no account"),
			http.StatusInternalServerError)
		return
	}

	if authenticatedUser != nil {
		if authenticatedUser.ID == verifiedUser.ID {
			if err := identity.SetAuthenticatedUser(w, r, verifiedUser); err != nil {
				logger.LogRequestError(r, errors.New("verification succeeded but the authenticated session could not be renewed"))
				util.HttpError(w, r, "Your account was verified, but the session could not be renewed. Please log in again.", nil, http.StatusInternalServerError)
				return
			}
		}
		// a user is currently authenticated - redirect to Home with a custom message
		authenticatedUserMessage := "Your account was verified successfully."
		if authenticatedUser.ID != verifiedUser.ID {
			authenticatedUserMessage = "The account was verified successfully, but is not currently logged in. Logout and login with the newly verified account for access."
		}

		err = flash.AddSuccessNotification(w, r, authenticatedUserMessage)
		if err != nil {
			logger.LogRequestError(r, errors.New("verification succeeded but its notification could not be saved"))
		}

		util.RedirectToURL(w, r, a.contentManager.HomeURL)

		return
	}

	// no user is authenticated - redirect the requestor to the login page
	err = flash.AddSuccessNotification(w, r, "Your account was verified successfully. Please login to access the site.")
	if err != nil {
		logger.LogRequestError(r, errors.New("verification succeeded but its notification could not be saved"))
	}

	util.RedirectToURL(w, r, a.contentManager.HomeURL)
}
