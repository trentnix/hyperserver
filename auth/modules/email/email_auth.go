// email_auth.go is a email/password authorization implementation of the AuthService
// interface
package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"net/http"

	"github.com/jmoiron/sqlx"
	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/components/user"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
)

// EmailAuthService implements the AuthService interface
type (
	EmailAuthService struct {
		db     *sqlx.DB
		config *config.Config

		AuthRedirect string

		loginButton    template.HTML
		registerButton template.HTML
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
)

const (
	AuthTypeEmail = "email"

	emailLoginFormTemplate          = "auth/modules/email/templates/html/login-form.html"
	emailLoginButtonTemplateName    = "auth/modules/email/templates/html/login-link.html"
	emailRegisterFormTemplate       = "auth/modules/email/templates/html/register-form.html"
	emailRegisterButtonTemplateName = "auth/modules/email/templates/html/register-link.html"
)

// init registers the AuthHandler handler with the application
func init() {
	auth_services.Register(new(EmailAuthService))
}

// Init initializes the EmailAuthService instance
func (a *EmailAuthService) Init(s *server.ApplicationServer) error {
	if s.Database == nil {
		return database.NewErrDatabaseUnavailable(errors.New("could not initialize EmailAuthService"))
	}

	a.db = s.Database
	a.config = s.Config

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

	if !login.IsHtmx() {
		// this should be an HTMX request - may need to consider adding support for
		// a layout template so that this could work even if JavaScript is disabled
		errMessage := "The Login form must be rendered via an HTMX request"
		content.RenderError(w, r, http.StatusBadRequest, errMessage)
		return
	}

	login.AddContentTemplate(content.Template(emailLoginFormTemplate))
	login.Data = &LoginForm{}

	err := login.Render(w, r)
	if err != nil {
		renderingError := fmt.Sprintf("Could not render the email auth login page: %s", err.Error())
		logger.LogRequestError(r, renderingError, err)
		content.RenderError(w, r, http.StatusInternalServerError, renderingError)
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
	if !login.IsHtmx() {
		// this should be an HTMX request - may need to consider adding support for
		// a layout template so that this could work even if JavaScript is disabled
		errMessage := "The Login action must be submitted via an HTMX request"
		content.RenderError(w, r, http.StatusBadRequest, errMessage)
		return nil
	}

	login.AddContentTemplate(emailLoginFormTemplate)
	loginForm := &LoginForm{}

	// extract login information, confirm the password, and authenticate the user
	if err := r.ParseForm(); err != nil {
		logger.LogRequestError(r, "there was an error parsing the login form data", err)
		content.RenderFormError(w, r, login, loginForm, "The login form data could not be parsed.")
		return nil
	}

	loginForm.Email = r.FormValue("email")
	loginForm.Password = r.FormValue("password")

	// validate the login form
	err := form.ValidateForm(loginForm)
	if err != nil {
		content.RenderFormError(w, r, login, loginForm, "The login form could not be validated")
		return nil
	}

	if loginForm.HasErrors() {
		// there are validation errors - render the form errors
		content.RenderFormError(w, r, login, loginForm, "")
		return nil
	}

	// authenticate the hs_user
	hs_user, err := user.GetUserByEmail(a.db, loginForm.Email)
	if err != nil && err != sql.ErrNoRows {
		content.RenderFormError(w, r, login, loginForm, "The user specified could not be retrieved from the database")
		return nil
	}

	if hs_user == nil || hs_user.Password == "" || !password.CheckPasswordHash(loginForm.Password, hs_user.Password) {
		content.RenderFormError(w, r, login, loginForm, "The provided login credentials are invalid")
		return nil
	}

	return hs_user
}

// GetRegister serves the register form
func (a *EmailAuthService) GetRegister(w http.ResponseWriter, r *http.Request) {
	register := content.NewManagedContent(r)

	if !register.IsHtmx() {
		// this should be an HTMX request - may need to consider adding support for
		// a layout template so that this could work even if JavaScript is disabled
		errMessage := "The Register form must be rendered via an HTMX request"
		content.RenderError(w, r, http.StatusBadRequest, errMessage)
		return
	}

	register.AddContentTemplate(content.Template(emailRegisterFormTemplate))
	register.Data = &RegisterForm{}

	err := register.Render(w, r)
	if err != nil {
		renderingError := fmt.Sprintf("Could not render the email auth register page: %s", err.Error())
		logger.LogRequestError(r, renderingError, err)
		content.RenderError(w, r, http.StatusInternalServerError, renderingError)
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
	if !register.IsHtmx() {
		// this should be an HTMX request - may need to consider adding support for
		// a layout template so that this could work even if JavaScript is disabled
		errMessage := "The Register action must be submitted via an HTMX request"
		content.RenderError(w, r, http.StatusBadRequest, errMessage)
		return false
	}

	register.AddContentTemplate(emailRegisterFormTemplate)
	registerForm := &RegisterForm{}

	// extract login information, confirm the password, and authenticate the user
	if err := r.ParseForm(); err != nil {
		logger.LogRequestError(r, "there was an error parsing the login form data", err)
		content.RenderFormError(w, r, register, registerForm, "The registration form data could not be parsed.")
		return false
	}

	registerForm.Email = r.FormValue("email")
	registerForm.Password = r.FormValue("password")
	registerForm.PasswordMatch = r.FormValue("passwordMatch")

	// validate the register form
	err := form.ValidateForm(registerForm)
	if err != nil {
		content.RenderFormError(w, r, register, registerForm, "The registration form could not be validated")
		return false
	}

	if registerForm.HasErrors() {
		// there are validation errors - render the form errors
		content.RenderFormError(w, r, register, registerForm, "")
		return false
	}

	//  validate user isn't already registered
	hs_user, err := user.GetUserByEmail(a.db, registerForm.Email)
	if err != nil && err != sql.ErrNoRows {
		content.RenderFormError(w, r, register, registerForm, "The user specified could not be retrieved from the database")
		return false
	}

	if hs_user != nil {
		// this user already exists
		content.RenderFormError(w, r, register, registerForm, "A user is already registered to the specified email address.")
		return false
	}

	hashedPassword, err := password.HashPassword(registerForm.Password)
	if err != nil {
		// the password could not be hashed
		content.RenderFormError(w, r, register, registerForm, "The specified account could not be created")
		return false
	}

	hs_user = &user.User{
		Email:                registerForm.Email,
		Password:             hashedPassword,
		RegistrationAuthType: AuthTypeEmail,
	}

	err = hs_user.Save(a.db)
	if err != nil {
		content.RenderFormError(w, r, register, registerForm, "The specified account could not be created")
		return false
	}

	content.AddSuccessMessage(r, w, "You have been successfully registered")

	return true
}
