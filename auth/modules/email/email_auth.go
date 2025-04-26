// email_auth.go is a email/password authorization implementation of the AuthService interface
package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/server"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// EmailAuthService implements the AuthService interface
type (
	EmailAuthService struct {
		db             *sqlx.DB
		config         *config.Config
		contentManager *content_services.ContentManagerService

		loginButton    template.HTML
		registerButton template.HTML

		host, port string
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

	// ResetPasswordRequestForm defines the fields used when requesting to reset a user's password
	ResetPasswordRequestForm struct {
		Email string `validate:"required,email"`

		form.Form
	}

	// ResetPasswordForm defines the fields used when resetting the password
	ResetPasswordForm struct {
		Password      string `validate:"required,password"`
		PasswordMatch string `validate:"required,password,eqfield=Password"`

		form.Form
	}

	ChangePasswordForm struct {
		OldPassword      string `validate:"required"`
		NewPassword      string `validate:"required,password,nefield=OldPassword"`
		NewPasswordMatch string `validate:"required,password,eqfield=NewPassword"`

		form.Form
	}
)

// Bind populates the LoginForm fields from the request.
func (lf *LoginForm) Bind(r *http.Request) error {
	lf.Email = r.FormValue("email")
	lf.Password = r.FormValue("password")
	return nil
}

// Bind populates the RegisterForm fields from the request.
func (rf *RegisterForm) Bind(r *http.Request) error {
	rf.Email = r.FormValue("email")
	rf.Password = r.FormValue("password")
	rf.PasswordMatch = r.FormValue("passwordMatch")
	return nil
}

// Bind populates the LoginForm fields from the request.
func (rprf *ResetPasswordRequestForm) Bind(r *http.Request) error {
	rprf.Email = r.FormValue("email")
	return nil
}

// Bind populates the RegisterForm fields from the request.
func (rpf *ResetPasswordForm) Bind(r *http.Request) error {
	rpf.Password = r.FormValue("password")
	rpf.PasswordMatch = r.FormValue("passwordMatch")
	return nil
}

// Bind populates the ChangePasswordForm fields from the request
func (cpf *ChangePasswordForm) Bind(r *http.Request) error {
	cpf.OldPassword = r.FormValue("oldPassword")
	cpf.NewPassword = r.FormValue("newPassword")
	cpf.NewPasswordMatch = r.FormValue("newPasswordMatch")
	return nil
}

const (
	AuthTypeEmail = "email"

	emailLoginFormTemplate          = "auth/modules/email/templates/html/login-form.html"
	emailLoginButtonTemplateName    = "auth/modules/email/templates/html/login-link.html"
	emailRegisterFormTemplate       = "auth/modules/email/templates/html/register-form.html"
	emailRegisterButtonTemplateName = "auth/modules/email/templates/html/register-link.html"
	emailResetRequestFormTemplate   = "auth/modules/email/templates/html/reset-request.html"
	emailResetPasswordFormTemplate  = "auth/modules/email/templates/html/reset-password.html"
	emailChangePasswordFormTemplate = "auth/modules/email/templates/html/change-password.html"

	emailLoginPath        = "/auth/login/email"
	emailRegisterPath     = "/auth/register/email"
	emailResetRequestPath = "/auth/reset/request/email"
	emailResetPath        = "/auth/reset/email"
	emailChangePath       = "/auth/change/email"
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
		return auth_services.NewErrEmailAuthServiceInit(errors.New("the email auth service is not enabled"))
	}

	if s.Database == nil {
		return auth_services.NewErrEmailAuthServiceInit(errors.New("the database service is not available"))
	}

	a.db = s.Database
	a.config = s.Config
	a.contentManager = s.ContentManager

	if a.config.Auth.JwtKey == "" {
		return auth_services.NewErrEmailAuthServiceInit(errors.New("the auth JWT key is not configured"))
	}

	var err error
	loginButtonTemplate := emailLoginButtonTemplateName
	a.loginButton, err = util.LoadHTMLFromFile(loginButtonTemplate)
	if err != nil {
		return auth_services.NewErrEmailAuthServiceInit(fmt.Errorf("could not find %s", loginButtonTemplate))
	}

	registerButtonTemplate := emailRegisterButtonTemplateName
	a.registerButton, err = util.LoadHTMLFromFile(registerButtonTemplate)
	if err != nil {
		return auth_services.NewErrEmailAuthServiceInit(fmt.Errorf("could not find %s", registerButtonTemplate))
	}

	if a.host = s.ContentManager.Host; a.host == "" {
		return auth_services.NewErrEmailAuthServiceInit(fmt.Errorf("application hostname not configured"))
	}

	if a.port = s.ContentManager.Port; a.port == "0" {
		// validating against 0 because the "zero" (unset) value for Port is 0
		return auth_services.NewErrEmailAuthServiceInit(fmt.Errorf("application port not configured"))
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

// GetRegisterButton returns the template.HTML object representing the way to start the
// registration process for email authorization
func (a *EmailAuthService) GetRegisterButton() template.HTML {
	return a.registerButton
}

// GetLogin serves the login form
func (a *EmailAuthService) GetLogin(w http.ResponseWriter, r *http.Request) {
	login := content.NewManagedContent(r, a.contentManager)
	login.AddContent(emailLoginFormTemplate)
	loginForm := &LoginForm{}
	loginForm.ActionUrl = emailLoginPath
	login.Data = loginForm

	err := login.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(w, r,
			"Unable to display the email authorization service login form",
			err,
			http.StatusInternalServerError)
	}
}

// Login handles a login request, validates the input, confirms the password matches the
// password stored in the database, and handles the user response.
//
// The logic flow is as follows:
//
//	Login page
//	  error -> back to login form with error displayed
//	  authentication failed -> back to login form with authentication failure displayed
//	  success -> redirect to the configured home page
func (a *EmailAuthService) Login(w http.ResponseWriter, r *http.Request) *user.User {
	login := content.NewManagedContent(r, a.contentManager)
	login.AddContent(emailLoginFormTemplate)
	loginForm := &LoginForm{}

	errMessage, err := form.ParseAndValidate(r, loginForm)
	if err != nil {
		form.HandleFormError(w, r, login, loginForm, errMessage, err)
		return nil
	}

	// if there are any validation errors, handle them and return an error.
	if loginForm.HasErrors() {
		form.HandleFormError(w, r, login, loginForm, "", nil)
		return nil
	}

	// authenticate the user
	u, err := user.GetUserByEmail(a.db, loginForm.Email)
	if err != nil && err != sql.ErrNoRows {
		form.HandleFormError(w, r,
			login,
			loginForm,
			"The user specified could not be retrieved from the database",
			err)
		return nil
	}

	if u == nil || u.Password == "" || !password.CheckPasswordHash(loginForm.Password, u.Password) {
		form.HandleFormError(w, r, login, loginForm, "Invalid login/password", nil)
		return nil
	}

	return u
}

// GetRegister serves the register form
func (a *EmailAuthService) GetRegister(w http.ResponseWriter, r *http.Request) {
	register := content.NewManagedContent(r, a.contentManager)
	register.AddContent(emailRegisterFormTemplate)
	registerForm := &RegisterForm{}
	registerForm.ActionUrl = emailRegisterPath
	register.Data = registerForm

	err := register.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(w, r,
			"Unable to display the email authorization service registration form",
			err,
			http.StatusInternalServerError)
	}
}

// Register handles a registration request, validates the input, creates a new user account, sends
// verification instructions (as configured), and handles the user response.
//
// The registration flow (TO DO) is as follows:
//
//	Register page
//	  error -> back to register page
//	  user already exists -> back to register page
//	  success -> redirect to login
func (a *EmailAuthService) Register(w http.ResponseWriter, r *http.Request) bool {
	register := content.NewManagedContent(r, a.contentManager)
	register.AddContent(emailRegisterFormTemplate)
	registerForm := &RegisterForm{}
	registerForm.ActionUrl = emailRegisterPath

	errMessage, err := form.ParseAndValidate(r, registerForm)
	if err != nil {
		form.HandleFormError(w, r, register, registerForm, errMessage, err)
		return false
	}

	// if there are any validation errors, handle them and return an error.
	if registerForm.HasErrors() {
		form.HandleFormError(w, r, register, registerForm, "", nil)
		return false
	}

	registrationErr, userMessage := auth_services.ProcessRegistration(a.db, registerForm.Email, registerForm.Password, AuthTypeEmail, a.config.Auth.RegisterRequiresVerification)
	if registrationErr != nil {
		form.HandleFormError(w, r, register, registerForm, userMessage, registrationErr)
		return false
	}

	return true
}

// GetResetRequest serves the password reset request page with the resetPasswordRequestForm form
func (a *EmailAuthService) GetResetRequest(w http.ResponseWriter, r *http.Request) {
	reset := content.NewManagedContent(r, a.contentManager)
	reset.AddContent(emailResetRequestFormTemplate)

	resetRequestForm := &ResetPasswordRequestForm{}
	resetRequestForm.ActionUrl = emailResetRequestPath
	reset.Data = resetRequestForm

	err := reset.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(w, r,
			"Unable to display the email authorization service password reset request form",
			err,
			http.StatusInternalServerError)
		return
	}
}

// ResetRequest processes a request to reset the password for the email specified in the
// submitted form. A successful result will notify the user of how to reset their password
// by creating a reset token. This token will be in a subsequent request to authenticate
// the reset request.
func (a *EmailAuthService) ResetRequest(w http.ResponseWriter, r *http.Request, tokenExpiration time.Duration) bool {
	// use a generic error for security reasons
	genericResetErrMsg := "There was an error trying to reset your password"

	reset := content.NewManagedContent(r, a.contentManager)
	reset.AddContent(emailResetRequestFormTemplate)
	resetRequestForm := &ResetPasswordRequestForm{}
	resetRequestForm.ActionUrl = emailResetPath

	errMessage, err := form.ParseAndValidate(r, resetRequestForm)
	if err != nil {
		form.HandleFormError(w, r, reset, resetRequestForm, errMessage, err)
		return false
	}

	// if there are any validation errors, handle them and return an error.
	if resetRequestForm.HasErrors() {
		form.HandleFormError(w, r, reset, resetRequestForm, "", nil)
		return false
	}

	//  validate user isn't already registered
	u, err := user.GetUserByEmail(a.db, resetRequestForm.Email)
	if err != nil && err != sql.ErrNoRows {
		// error getting the user
		form.HandleFormError(w, r, reset, resetRequestForm, genericResetErrMsg, err)
		return false
	}

	if u == nil {
		// the specified user does not exist
		form.HandleFormError(w, r, reset, resetRequestForm, genericResetErrMsg, nil)
		return false
	}

	// the specified user exists - generate a password token and save it to the database
	token, err := user.NewAuthResetToken(u, []byte(a.config.Auth.JwtKey), tokenExpiration)
	if err != nil {
		form.HandleFormError(w, r, reset, resetRequestForm, genericResetErrMsg, err)
		return false
	}

	// add hashed token to the database (with user.id and expiration)
	err = token.Create(a.db)
	if err != nil {
		form.HandleFormError(w, r, reset, resetRequestForm, genericResetErrMsg, err)
		return false
	}

	resetRequestForm.Email = ""

	params := map[string]string{"token": token.Token}
	resetUrl, err := util.BuildUrl(r, a.host, a.port, emailResetPath, params)
	if err != nil {
		a.contentManager.HandleError(w, r,
			genericResetErrMsg,
			form.NewErrActionNotSpecified(fmt.Errorf("error creating the reset path"), resetRequestForm),
			http.StatusInternalServerError)
		return false
	}

	successMessage := fmt.Sprintf(`<a href="%s">Click here</a> to reset your password.`, resetUrl.String())
	resetRequestForm.SetFormMessage(successMessage)

	resetRequestForm.ActionUrl = emailResetRequestPath
	reset.Data = resetRequestForm
	err = reset.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(
			w,
			r,
			"Unable to display the email authorization service reset request form",
			err,
			http.StatusInternalServerError)
		return false
	}

	return true
}

// GetReset serves the password reset page with the resetPasswordForm form
func (a *EmailAuthService) GetReset(w http.ResponseWriter, r *http.Request, token string) {
	resetErrMsg := "Unable to display the email authorization service password reset form"

	if token == "" {
		a.contentManager.HandleError(w, r, resetErrMsg, user.NewErrTokenNotSpecified(nil), http.StatusInternalServerError)
		return
	}

	resetForm := &ResetPasswordForm{}

	params := map[string]string{"token": token}
	actionUrl, buildUrlErr := util.BuildUrl(r, a.host, a.port, emailResetPath, params)
	if buildUrlErr != nil {
		a.contentManager.HandleError(w, r,
			resetErrMsg,
			form.NewErrActionNotSpecified(nil, resetForm),
			http.StatusInternalServerError)
		return
	}

	resetForm.ActionUrl = actionUrl.String()

	reset := content.NewManagedContent(r, a.contentManager)
	reset.AddContent(emailResetPasswordFormTemplate)
	reset.Data = resetForm

	err := reset.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(w, r, resetErrMsg, err, http.StatusInternalServerError)
		return
	}
}

// Reset processes a password reset request
func (a *EmailAuthService) Reset(
	w http.ResponseWriter,
	r *http.Request,
	u *user.User,
	token string,
	requireNewCredentials bool,
) bool {
	reset := content.NewManagedContent(r, a.contentManager)
	reset.AddContent(emailResetPasswordFormTemplate)

	resetForm := &ResetPasswordForm{}
	reset.Data = resetForm

	if u == nil {
		form.HandleFormError(
			w,
			r,
			reset,
			resetForm,
			"Unable to reset your password due to an internal error",
			user.NewErrUserNotSpecified(fmt.Errorf("reset failed")))
		return false
	}

	errMessage, err := form.ParseAndValidate(r, resetForm)
	if err != nil {
		form.HandleFormError(w, r, reset, resetForm, errMessage, err)
		return false
	}

	// if there are any validation errors, handle them and return an error.
	if resetForm.HasErrors() {
		form.HandleFormError(w, r, reset, resetForm, "", nil)
		return false
	}

	params := map[string]string{"token": token}
	actionUrl, buildUrlErr := util.BuildUrl(r, a.host, a.port, emailResetPath, params)
	if buildUrlErr != nil {
		a.contentManager.HandleError(w, r,
			"Unable to display the email authorization service reset request form",
			form.NewErrActionNotSpecified(buildUrlErr, resetForm),
			http.StatusInternalServerError)
		return false
	}

	resetForm.ActionUrl = actionUrl.String()

	if requireNewCredentials {
		if u.Password != "" && password.CheckPasswordHash(resetForm.Password, u.Password) {
			// the new password is the same as the old, but a new password is required according
			// to the configuration
			form.HandleFormError(w, r,
				reset,
				resetForm,
				"The provided password has been recently used. For security reasons, a new password is required.",
				nil)
			return false
		}
	}

	genericResetErrMsg := "Unable to reset password"

	hashedPassword, err := password.HashPassword(resetForm.Password)
	if err != nil {
		// the password could not be hashed
		form.HandleFormError(w, r, reset, resetForm, genericResetErrMsg, err)
		return false
	}

	u.Password = hashedPassword
	err = u.Save(a.db)
	if err != nil {
		// the user could not be updated
		form.HandleFormError(
			w,
			r,
			reset,
			resetForm,
			genericResetErrMsg,
			fmt.Errorf("error updating a user's password during reset: %w", err))
		return false
	}

	return true
}

// GetChange serves the password change page with the changePasswordForm form
func (a *EmailAuthService) GetChange(w http.ResponseWriter, r *http.Request) {
	changeErrMsg := "Unable to display the email authorization service password change form"

	changeForm := &ChangePasswordForm{}
	changeForm.ActionUrl = emailChangePath

	change := content.NewManagedContent(r, a.contentManager)
	change.AddContent(emailChangePasswordFormTemplate)
	change.Data = changeForm

	err := change.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(w, r, changeErrMsg, err, http.StatusInternalServerError)
		return
	}
}

// Change authenticates the specified user and, if authentication succeeds and a new
// password meets the password complexity criteria, changes the user's password to a new value.
func (a *EmailAuthService) Change(w http.ResponseWriter, r *http.Request, u *user.User) bool {
	changeErrMsg := "unable to change your password"

	change := content.NewManagedContent(r, a.contentManager)
	change.AddContent(emailChangePasswordFormTemplate)

	changeForm := &ChangePasswordForm{}
	change.Data = changeForm

	if u == nil {
		form.HandleFormError(
			w,
			r,
			change,
			changeForm,
			"Unable to change your password due to an internal error",
			user.NewErrUserNotSpecified(fmt.Errorf("change failed")))
		return false
	}

	errMessage, err := form.ParseAndValidate(r, changeForm)
	if err != nil {
		form.HandleFormError(w, r, change, changeForm, errMessage, err)
		return false
	}

	// if there are any validation errors, handle them and return an error.
	if changeForm.HasErrors() {
		form.HandleFormError(w, r, change, changeForm, "", nil)
		return false
	}

	if u.Password != "" && !password.CheckPasswordHash(changeForm.OldPassword, u.Password) {
		// the old password isn't correct - it is needed to change to a new password
		form.HandleFormError(
			w,
			r,
			change,
			changeForm,
			"The provided password is incorrect",
			nil)
		return false
	}

	hashedPassword, err := password.HashPassword(changeForm.NewPassword)
	if err != nil {
		// the password could not be hashed
		form.HandleFormError(
			w,
			r,
			change,
			changeForm,
			changeErrMsg,
			fmt.Errorf("Unable to hash the NewPassword value"))
		return false
	}

	u.Password = hashedPassword
	err = u.Save(a.db)
	if err != nil {
		// the user could not be updated
		form.HandleFormError(
			w,
			r,
			change,
			changeForm,
			changeErrMsg,
			fmt.Errorf("Unable to save the updated password: %w", err))
		return false
	}

	changeForm.OldPassword = ""
	changeForm.NewPassword = ""
	changeForm.NewPasswordMatch = ""
	changeForm.SetFormMessage("Your password has been changed.")

	err = change.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(
			w,
			r,
			"The password was updated, but there was an error displaying the change form.",
			err,
			http.StatusInternalServerError)
		return false
	}

	return true
}
