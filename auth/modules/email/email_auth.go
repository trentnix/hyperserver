// email_auth.go is a email/password authorization implementation of the AuthService
// interface
package auth

import (
	"database/sql"
	"errors"
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
)

// EmailAuthService implements the AuthService interface
type (
	EmailAuthService struct {
		db     *sqlx.DB
		config *config.Config

		AuthRedirect string
	}

	// LoginForm defines the fields used when logging in via email/password
	LoginForm struct {
		Email    string `validate:"required,email"`
		Password string

		form.Form
	}
)

const (
	AuthTypeEmail = "email"

	emailContainerTemplateName = "/auth/email/container"
	emailLoginFormTemplate     = "auth/modules/email/templates/html/login-form.html"
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

// GetLogin serves the login page with the login form
func (a *EmailAuthService) GetLogin(w http.ResponseWriter, r *http.Request) {
	login := content.NewManagedContent(r)

	if !login.IsHtmx() {
		// this should be an HTMX request - may need to consider adding support for
		// a layout template so that this could work even if JavaScript is disabled
		contentManager := content.GetContentManager()
		contentManager.ErrorHandler(
			w,
			r,
			http.StatusBadRequest,
			"The Login form must be rendered via an HTMX request")
		return
	}

	login.AddContentTemplate(content.Template(emailLoginFormTemplate))
	login.Data = &LoginForm{}

	err := login.Render(w, r)
	if err != nil {
		http.Error(w, "the login form won't render", http.StatusInternalServerError)
	}
}

// Login handles a login request, valides the input, confirms the password matches the
// password stored in the database, and redirects the user back to configured auth landing page.
//
// The logic flow is as follows:
//
//	Login page
//	  error -> back to login page with error displayed
//	  authentication failed -> back to login with authentication failure displayed
//	  user verification is required but the authenciated user isn't verified -> verification required page
//	  success -> redirect to the configured landing page (defaults to '/')
func (a *EmailAuthService) Login(w http.ResponseWriter, r *http.Request) bool {
	login := content.NewManagedContent(r)
	if !login.IsHtmx() {
		// this should be an HTMX request - may need to consider adding support for
		// a layout template so that this could work even if JavaScript is disabled
		errMessage := "The Login form must be rendered via an HTMX request"
		content.RenderError(w, r, http.StatusBadRequest, errMessage)
		return false
	}

	login.AddContentTemplate(emailLoginFormTemplate)
	LoginForm := &LoginForm{}

	// extract login information, confirm the password, and authenticate the user
	if err := r.ParseForm(); err != nil {
		logger.LogRequestError(r, "there was an error parsing the login form data", err)
		content.RenderFormError(w, r, login, LoginForm, "The login form data could not be parsed.")
		return false
	}

	LoginForm.Email = r.FormValue("email")
	LoginForm.Password = r.FormValue("password")

	// validate the login form
	err := form.ValidateForm(LoginForm)
	if err != nil {
		content.RenderFormError(w, r, login, LoginForm, "The login form could not be validated")
		return false
	}

	if LoginForm.HasErrors() {
		// there are validation errors - render the form errors
		content.RenderFormError(w, r, login, LoginForm, "")
		return false
	}

	// authenticate the hs_user
	hs_user, err := user.GetUserByEmail(a.db, LoginForm.Email)
	if err != nil && err != sql.ErrNoRows {
		content.RenderFormError(w, r, login, LoginForm, "The user specified could not be retrieved from the database")
		return false
	}

	if hs_user == nil || hs_user.Password == "" || !password.CheckPasswordHash(LoginForm.Password, hs_user.Password) {
		content.RenderFormError(w, r, login, LoginForm, "The provided login credentials are invalid")
		return false
	}

	setAuthenticatedUserErr := user.SetAuthenticatedUser(r, w, hs_user)
	if setAuthenticatedUserErr != nil {
		logger.LogRequestError(r, "Could not save the newly authenticated user to a session", setAuthenticatedUserErr)
		content.RenderFormError(w, r, login, LoginForm, "There was an internal error when trying to login")
		return false
	}

	r = content.AddUserSuccessMessage(r, "You have been successfully logged in")

	content.RenderHome(w, r)

	return true
}
