package auth

import (
	"context"
	"errors"
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// LoginAvailable reports whether the account has password credentials.
func (*EmailAuthService) LoginAvailable(_ context.Context, account *user.User) (bool, error) {
	return account.Password != "", nil
}

const (
	emailLoginFormTemplate       = "templates/html/partials/login-form.html"
	emailLoginButtonTemplateName = "templates/html/partials/login-link.html"
	emailLoginFormPartial        = "auth.partial.email.login.form"
	emailLoginPath               = "/auth/login/email"
)

// LoginForm defines the fields used when logging in via email/password
type LoginForm struct {
	Email        string
	Password     string
	Alternatives []template.HTML

	form.Form
}

// Bind populates the login email address and password from r.
func (lf *LoginForm) Bind(r *http.Request) error {
	lf.Email = r.FormValue("email")
	lf.Password = r.FormValue("password")
	return nil
}

// Validate checks the login email. Existing passwords need not meet creation rules.
func (lf *LoginForm) Validate() form.FieldErrors {
	return validateEmail(lf.Email)
}

// GetLoginButton returns the template.HTML object representing the way to start the login process
// for email authorization
func (a *EmailAuthService) GetLoginButton() template.HTML {
	return a.loginButton
}

// GetLoginForm renders email credentials alongside other enabled providers.
func (a *EmailAuthService) GetLoginForm(w http.ResponseWriter, r *http.Request) {
	a.GetLogin(w, r)
}

// GetLogin serves the login form
func (a *EmailAuthService) GetLogin(w http.ResponseWriter, r *http.Request) {
	loginForm := &LoginForm{Alternatives: auth.LoginAlternatives(r)}
	loginForm.ActionUrl = emailLoginPath
	login := a.views[emailLoginFormPartial].Content(r, loginForm)

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	loginForm.SetMessages(authMessages)

	err = login.Render(w, r)
	if err != nil {
		login.HandleRenderError(w, r,
			"Unable to display the email authorization service login form",
			err)
	}
}

// Login validates credentials and renders failures. The auth manager establishes
// the session and redirects after a successful login.
func (a *EmailAuthService) Login(w http.ResponseWriter, r *http.Request) (account *user.User) {
	defer func() {
		if account == nil {
			logger.Audit(r.Context(), "login", "rejected")
		}
	}()
	loginForm := &LoginForm{Alternatives: auth.LoginAlternatives(r)}
	loginForm.ActionUrl = emailLoginPath
	login := a.views[emailLoginFormPartial].Content(r, loginForm)

	err := form.ParseAndValidate(r, loginForm)
	if err != nil {
		form.HandleFormError(w, r, login, loginForm, "Unable to process form data", err)
		return nil
	}

	if !a.Policy.Admit(w, r, loginForm.Email, a.contentManager) {
		return nil
	}
	u, err := a.authenticate(r.Context(), loginForm.Email, loginForm.Password)
	if errors.Is(err, errInvalidCredentials) {
		form.HandleFormError(w, r, login, loginForm, "Invalid login/password", nil)
		return nil
	}
	if err != nil {
		form.HandleFormError(w, r,
			login,
			loginForm,
			"The user specified could not be retrieved from the database",
			err)
		return nil
	}

	return u
}
