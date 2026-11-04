package auth

import (
	"context"
	"errors"
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// LoginAvailable reports whether the account has password credentials.
func (*EmailAuthService) LoginAvailable(_ context.Context, account *user.User) (bool, error) {
	return account.Password != "", nil
}

const (
	emailLoginFormTemplate       = "templates/html/partials/login-form.html"
	emailLoginButtonTemplateName = "templates/html/partials/login-link.html"
	emailLoginFormPartial        = "auth.partial.email.login.form"
	emailLoginPath               = "/login"
)

// LoginForm defines the fields used when logging in via email/password
type LoginForm struct {
	Email    string
	Password string

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

// LoginForm returns escaped controls for the shared login page.
func (a *EmailAuthService) LoginForm(r *http.Request) (template.HTML, error) {
	return a.renderLoginForm(r, &LoginForm{})
}

func (a *EmailAuthService) renderLoginForm(r *http.Request, f *LoginForm) (template.HTML, error) {
	f.ActionUrl = emailLoginPath
	f.Password = ""
	return a.views[emailLoginFormPartial].RenderPartial(r, f)
}

// Login validates credentials. The shared manager renders validation failures
// with the other enabled providers and establishes the session on success.
func (a *EmailAuthService) Login(w http.ResponseWriter, r *http.Request) (result auth.LoginResult) {
	defer func() {
		if result.Account == nil {
			logger.Audit(r.Context(), "login", "rejected")
		}
	}()
	f := &LoginForm{}
	err := form.ParseAndValidate(r, f)
	if err == nil {
		if !a.Policy.Admit(w, r, f.Email, a.contentManager) {
			return result
		}
		result.Account, err = a.authenticate(r.Context(), f.Email, f.Password)
		if err == nil {
			return result
		}
	}
	var parseErr *form.ParseError
	var fields form.FieldErrors
	switch {
	case errors.As(err, &parseErr):
		util.HttpError(w, r, parseErr.Error(), nil, parseErr.StatusCode)
		return result
	case errors.As(err, &fields):
		// ParseAndValidate already recorded the field errors.
	case errors.Is(err, errInvalidCredentials):
		f.AddErrorMessage("Invalid login/password")
	default:
		logger.LogRequestError(r, err)
		f.AddErrorMessage("The user specified could not be retrieved from the database")
	}
	result.Form, err = a.renderLoginForm(r, f)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to display the login form.", err, http.StatusInternalServerError)
		return auth.LoginResult{}
	}
	return result
}
