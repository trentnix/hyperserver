// email_auth.go is a email/password authorization implementation of the AuthService
// interface
package auth

import (
	"database/sql"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// EmailAuthService implements the AuthService interface
type (
	EmailAuthService struct {
		db     *sqlx.DB
		config *config.Config

		loginButton    template.HTML
		registerButton template.HTML

		host string
	}

	// LoginForm defines the fields used when logging in via email/password
	LoginForm struct {
		Email    string `validate:"required,email"`
		Password string

		form.Form
	}

	// registerForm defines the fields used when registering a new user in via email/password
	RegisterForm struct {
		Email         string `validate:"required,email"`
		Password      string `validate:"required,password"`
		PasswordMatch string `validate:"required,password,eqfield=Password"`

		form.Form
	}

	// resetPasswordRequestForm defines the fields used when requesting to reset a user's password
	resetPasswordRequestForm struct {
		Email string `validate:"required,email"`

		form.Form
	}

	// resetPasswordForm defines the fields used when resetting the password
	resetPasswordForm struct {
		Password      string `validate:"required,password"`
		PasswordMatch string `validate:"required,password,eqfield=Password"`
		Token         string

		form.Form
	}

	changePasswordForm struct {
		OldPassword      string `validate:"required"`
		NewPassword      string `validate:"required,password,nefield=OldPassword"`
		NewPasswordMatch string `validate:"required,password,eqfield=NewPassword"`

		form.Form
	}
)

const (
	AuthTypeEmail = "email"

	emailLoginFormTemplate          = "auth/modules/email/templates/html/login-form.html"
	emailLoginButtonTemplateName    = "auth/modules/email/templates/html/login-link.html"
	emailRegisterFormTemplate       = "auth/modules/email/templates/html/register-form.html"
	emailRegisterButtonTemplateName = "auth/modules/email/templates/html/register-link.html"
	emailResetRequestFormTemplate   = "auth/modules/email/templates/html/reset-request.html"
	emailResetPasswordFormTemplate  = "auth/modules/email/templates/html/reset-password.html"
)

// init registers the AuthHandler handler with the application
func init() {
	auth_services.Register(new(EmailAuthService))
}

// Init initializes the EmailAuthService instance
func (a *EmailAuthService) Init(s *server.ApplicationServer) error {
	configOptions := auth_services.GetAuthConfigOptions(s.Config, AuthTypeEmail)
	authServiceEnabled := strings.EqualFold(configOptions["enabled"], "true") || configOptions["enabled"] == "1"
	if !authServiceEnabled {
		return auth_services.NewErrAuthServiceDisabled(fmt.Errorf("unable to initialize the email auth service"), AuthTypeEmail)
	}

	if s.Database == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("unable to initialize EmailAuthService"))
	}

	a.db = s.Database
	a.config = s.Config

	if a.config.Auth.JwtKey == "" {
		return fmt.Errorf("the auth JWT key was not set")
	}

	var err error
	loginButtonTemplate := emailLoginButtonTemplateName
	a.loginButton, err = util.LoadHTMLFromFile(loginButtonTemplate)
	if err != nil {
		return fmt.Errorf("could not find %s", loginButtonTemplate)
	}

	registerButtonTemplate := emailRegisterButtonTemplateName
	a.registerButton, err = util.LoadHTMLFromFile(registerButtonTemplate)
	if err != nil {
		return fmt.Errorf("could not find %s", registerButtonTemplate)
	}

	a.host = a.config.HTTP.Hostname

	if a.config.HTTP.Port != 80 && a.config.HTTP.Port != 0 {
		port := strconv.Itoa(int(a.config.HTTP.Port))
		a.host += ":" + port
	}

	return nil
}

// Routes is implemented to fulfill the AuthService interface but is not supported by
// the EmailAuthService implementation
func (a *EmailAuthService) Routes(mux *http.ServeMux) {
	// none needed for EmailAuthService
}

// IsValid checks the configuration to ensure the EmailAuthService instance is configured correctly
func (a *EmailAuthService) IsLoaded() bool {
	return true
}

// AuthType returns the auth type for this AuthService implementation
func (a *EmailAuthService) AuthType() string {
	return AuthTypeEmail
}

// GetLoginButton returns the template.HTML object representing the way to start the login process
// for email authorization
func (a *EmailAuthService) GetLoginButton() template.HTML {
	return a.loginButton
}

// GetLoginButton returns the template.HTML object representing the way to start the login process
// for email authorization
func (a *EmailAuthService) GetRegisterButton() template.HTML {
	return a.registerButton
}

// GetLogin serves the login form
func (a *EmailAuthService) GetLogin(w http.ResponseWriter, r *http.Request) {
	login := content.NewManagedContent(r)
	login.AddContent(content.TemplatePath(emailLoginFormTemplate))
	login.Data = &LoginForm{}

	err := login.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "could not render the login form in the email authorization service", err, http.StatusInternalServerError)
	}
}

// Login handles a login request, valides the input, confirms the password matches the
// password stored in the database, and handles the user response.
//
// The logic flow is as follows:
//
//	Login page
//	  error -> back to login form with error displayed
//	  authentication failed -> back to login form with authentication failure displayed
//	  success -> redirect to the configured home page
func (a *EmailAuthService) Login(w http.ResponseWriter, r *http.Request) *user.User {
	login := content.NewManagedContent(r)
	login.AddContent(emailLoginFormTemplate)
	loginForm := &LoginForm{}

	// extract login information, confirm the password, and authenticate the user
	if err := r.ParseForm(); err != nil {
		content.HandleFormError(w, r, login, loginForm, "The login form data could not be parsed.")
		return nil
	}

	loginForm.Email = r.FormValue("email")
	loginForm.Password = r.FormValue("password")

	// validate the login form
	err := form.Validate(loginForm)
	if err != nil {
		content.HandleFormError(w, r, login, loginForm, "The login form could not be validated")
		return nil
	}

	if loginForm.HasErrors() {
		// there are validation errors - render the form errors
		content.HandleFormError(w, r, login, loginForm, "")
		return nil
	}

	// authenticate the hs_user
	hs_user, err := user.GetUserByEmail(a.db, loginForm.Email)
	if err != nil && err != sql.ErrNoRows {
		content.HandleFormError(w, r, login, loginForm, "The user specified could not be retrieved from the database")
		return nil
	}

	if hs_user == nil || hs_user.Password == "" || !password.CheckPasswordHash(loginForm.Password, hs_user.Password) {
		content.HandleFormError(w, r, login, loginForm, "The provided login credentials are invalid")
		return nil
	}

	return hs_user
}

// GetRegister serves the register form
func (a *EmailAuthService) GetRegister(w http.ResponseWriter, r *http.Request) {
	register := content.NewManagedContent(r)
	register.AddContent(content.TemplatePath(emailRegisterFormTemplate))
	register.Data = &RegisterForm{}

	err := register.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "could not render the registration form in the email authentication service", err, http.StatusInternalServerError)
	}
}

// Register handles a registration request, valides the input, creates a new user account, sends
// verification instructions (as configured), and handles the user response.
//
// The registration flow (TO DO) is as follows:
//
//	Register page
//	  error -> back to register page
//	  user already exists -> back to register page
//	  success -> redirect to login
func (a *EmailAuthService) Register(w http.ResponseWriter, r *http.Request) bool {
	register := content.NewManagedContent(r)
	register.AddContent(emailRegisterFormTemplate)
	registerForm := &RegisterForm{}

	// extract login information, confirm the password, and authenticate the user
	if err := r.ParseForm(); err != nil {
		logger.LogRequestError(r, fmt.Errorf("there was an error parsing the login form data: %w", err))
		content.HandleFormError(w, r, register, registerForm, "The registration form data could not be parsed.")
		return false
	}

	registerForm.Email = r.FormValue("email")
	registerForm.Password = r.FormValue("password")
	registerForm.PasswordMatch = r.FormValue("passwordMatch")

	// validate the register form
	err := form.Validate(registerForm)
	if err != nil {
		content.HandleFormError(w, r, register, registerForm, "The registration form could not be validated")
		return false
	}

	if registerForm.HasErrors() {
		// there are validation errors - render the form errors
		content.HandleFormError(w, r, register, registerForm, "")
		return false
	}

	//  validate user isn't already registered
	hs_user, err := user.GetUserByEmail(a.db, registerForm.Email)
	if err != nil && err != sql.ErrNoRows {
		content.HandleFormError(w, r, register, registerForm, "The user specified could not be retrieved from the database")
		return false
	}

	if hs_user != nil {
		// this user already exists
		content.HandleFormError(w, r, register, registerForm, "A user is already registered to the specified email address.")
		return false
	}

	hashedPassword, err := password.HashPassword(registerForm.Password)
	if err != nil {
		// the password could not be hashed
		content.HandleFormError(w, r, register, registerForm, "The specified account could not be created")
		return false
	}

	hs_user = &user.User{
		Email:                registerForm.Email,
		Password:             hashedPassword,
		RegistrationAuthType: AuthTypeEmail,
	}

	err = hs_user.Save(a.db)
	if err != nil {
		content.HandleFormError(w, r, register, registerForm, "The specified account could not be created")
		return false
	}

	return true
}

// GetResetRequest serves the password reset request page with the resetPasswordRequestForm form
func (a *EmailAuthService) GetResetRequest(w http.ResponseWriter, r *http.Request) {
	reset := content.NewManagedContent(r)
	reset.AddContent(content.TemplatePath(emailResetRequestFormTemplate))
	reset.Data = &resetPasswordRequestForm{}

	err := reset.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "could not render the reset request form in the email authentication service", err, http.StatusInternalServerError)
		return
	}
}

// ResetRequest processes a request to reset the password for the email specified in the
// submitted form. A successful result will notify the user of how to reset their password
// by creating a reset token. This token will be in a subsequent request to authenticate
// the reset request.
func (a *EmailAuthService) ResetRequest(w http.ResponseWriter, r *http.Request, resetTokenExpiration time.Duration) bool {
	genericResetErrMsg := "There was an error trying to reset the specified user's password."

	reset := content.NewManagedContent(r)
	reset.AddContent(emailResetRequestFormTemplate)
	resetRequestForm := &resetPasswordRequestForm{}

	// extract login information, confirm the password, and authenticate the user
	if err := r.ParseForm(); err != nil {
		logger.LogRequestError(r, fmt.Errorf("there was an error parsing the reset request form data: %w", err))
		content.HandleFormError(w, r, reset, resetRequestForm, "The reset request form data could not be parsed.")
		return false
	}

	resetRequestForm.Email = r.FormValue("email")

	// validate the register form
	err := form.Validate(resetRequestForm)
	if err != nil {
		content.HandleFormError(w, r, reset, resetRequestForm, "The reset password request form could not be validated")
		return false
	}

	if resetRequestForm.HasErrors() {
		// there are validation errors - render the form errors
		content.HandleFormError(w, r, reset, resetRequestForm, "")
		return false
	}

	//  validate user isn't already registered
	hs_user, err := user.GetUserByEmail(a.db, resetRequestForm.Email)
	if err != nil && err != sql.ErrNoRows {
		logger.LogRequestError(r, err)
		content.HandleFormError(w, r, reset, resetRequestForm, genericResetErrMsg)
		return false
	}

	if hs_user == nil {
		// this user does not exist
		content.HandleFormError(w, r, reset, resetRequestForm, genericResetErrMsg)
		return false
	}

	// the specified user exists - generate a password token and save it to the database
	token, err := user.NewPasswordResetToken(hs_user, []byte(a.config.Auth.JwtKey), resetTokenExpiration)
	if err != nil {
		logger.LogRequestError(r, err)
		content.HandleFormError(w, r, reset, resetRequestForm, genericResetErrMsg)
		return false
	}

	// add hashed token to the database (with user.id and expiration)
	err = token.Create(a.db)
	if err != nil {
		logger.LogRequestError(r, err)
		content.HandleFormError(w, r, reset, resetRequestForm, genericResetErrMsg)
		return false
	}

	// send the password reset information to the user
	resetURL := fmt.Sprintf("%s/auth/reset/email?token=%s", a.host, url.QueryEscape(token.Token))
	if r.TLS != nil {
		resetURL = "https://" + resetURL
	} else {
		resetURL = "http://" + resetURL
	}
	// TO DO - send email with URL

	resetRequestForm.Email = ""

	successMessage := fmt.Sprintf(`<a href="%s">Click here</a> to reset your password.`, html.EscapeString(resetURL))
	resetRequestForm.SetFormMessage(successMessage)

	reset.Data = resetRequestForm
	err = reset.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "could not render the reset request form in the email authentication service", err, http.StatusInternalServerError)
		return false
	}

	return true
}

// GetReset serves the password reset page with the resetPasswordForm form
func (a *EmailAuthService) GetReset(w http.ResponseWriter, r *http.Request, token string) {
	resetForm := &resetPasswordForm{
		Token: token,
	}

	reset := content.NewManagedContent(r)
	reset.AddContent(content.TemplatePath(emailResetPasswordFormTemplate))
	reset.Data = resetForm

	err := reset.Render(w, r)
	if err != nil {
		content.HandleError(w, r, "could not render the reset password form in the email authentication service", err, http.StatusInternalServerError)
		return
	}
}

func (a *EmailAuthService) Reset(w http.ResponseWriter, r *http.Request, u *user.User, token string, resetRequiresNewCredentials bool) bool {
	reset := content.NewManagedContent(r)
	reset.AddContent(emailResetPasswordFormTemplate)
	resetForm := &resetPasswordForm{}

	reset.Data = resetForm

	if u == nil {
		logger.LogRequestError(r, user.NewErrUserNotSpecified(fmt.Errorf("reset failed")))
		content.HandleFormError(w, r, reset, resetForm, "Reset failed - the user is not specified")
		return false
	}

	// extract login information, confirm the password, and authenticate the user
	if err := r.ParseForm(); err != nil {
		logger.LogRequestError(r, fmt.Errorf("there was an error parsing the reset form data: %w", err))
		content.HandleFormError(w, r, reset, resetForm, "The reset form data could not be parsed.")
		return false
	}

	resetForm.Password = r.FormValue("password")
	resetForm.PasswordMatch = r.FormValue("passwordMatch")
	resetForm.Token = token

	// validate the register form
	err := form.Validate(resetForm)
	if err != nil {
		content.HandleFormError(w, r, reset, resetForm, "The reset form could not be validated")
		return false
	}

	if resetForm.HasErrors() {
		// there are validation errors - render the form errors
		content.HandleFormError(w, r, reset, resetForm, "")
		return false
	}

	if resetRequiresNewCredentials {
		if u.Password != "" && password.CheckPasswordHash(resetForm.Password, u.Password) {
			// the new password is the same as the old, but a new password is required according
			// to the configuration
			content.HandleFormError(w, r, reset, resetForm, "The provided password is already in use - a new password is required")
			return false
		}
	}

	genericResetErrMsg := "Unable to reset password"

	hashedPassword, err := password.HashPassword(resetForm.Password)
	if err != nil {
		// the password could not be hashed
		content.HandleFormError(w, r, reset, resetForm, genericResetErrMsg)
		return false
	}

	u.Password = hashedPassword
	err = u.Save(a.db)
	if err != nil {
		// the user could not be updated
		logger.LogRequestError(r, fmt.Errorf("error updating a user's password during reset: %w", err))
		content.HandleFormError(w, r, reset, resetForm, genericResetErrMsg)
		return false
	}

	return true
}
