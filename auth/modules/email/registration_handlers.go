package auth

import (
	"errors"
	"html/template"
	"net/http"

	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const (
	emailRegisterFormTemplate       = "templates/html/partials/register-form.html"
	emailRegisterButtonTemplateName = "templates/html/partials/register-link.html"
	emailRegisterFormPartial        = "auth.partial.email.register.form"
	emailRegisterPath               = "/register"
)

// RegisterForm binds an email address and matching passwords for account creation.
type RegisterForm struct {
	Email         string
	Password      string
	PasswordMatch string

	form.Form
}

// Bind populates the registration email, password, and confirmation from r.
func (rf *RegisterForm) Bind(r *http.Request) error {
	rf.Email = r.FormValue("email")
	rf.Password = r.FormValue("password")
	rf.PasswordMatch = r.FormValue("passwordMatch")
	return nil
}

// Validate checks the registration email and matching passwords.
func (rf *RegisterForm) Validate() form.FieldErrors {
	errors := validateEmail(rf.Email)
	validatePasswords(errors, "Password", "PasswordMatch", rf.Password, rf.PasswordMatch)
	return errors
}

// GetRegisterButton returns the template.HTML object representing the way to start the
// registration process for email authorization
func (a *EmailAuthService) GetRegisterButton() template.HTML {
	if !a.config.Auth.Enabled || !a.config.Auth.RegistrationEnabled {
		return ""
	}
	return a.registerButton
}

// RegistrationForm returns escaped controls for the shared registration page.
func (a *EmailAuthService) RegistrationForm(r *http.Request) (template.HTML, error) {
	f := &RegisterForm{}
	token, err := user.RegistrationInvitation(r.Context(), a.accounts, "")
	if err != nil {
		return "", err
	}
	if token != nil {
		f.Email = token.Email
	}
	return a.renderRegistrationForm(r, f)
}

func (a *EmailAuthService) renderRegistrationForm(r *http.Request, f *RegisterForm) (template.HTML, error) {
	f.ActionUrl = emailRegisterPath
	f.Password, f.PasswordMatch = "", ""
	return a.views[emailRegisterFormPartial].RenderPartial(r, f)
}

// Register validates input, creates an account, and sends verification instructions
// when required. Validation failures return controls for the shared page. After
// account creation, it owns the response, including verification delivery failures.
func (a *EmailAuthService) Register(w http.ResponseWriter, r *http.Request) (result auth_services.RegistrationResult) {
	if !a.config.Auth.Enabled || !a.config.Auth.RegistrationEnabled {
		auth_services.RegistrationUnavailable(w, r)
		return
	}

	registerForm := &RegisterForm{}
	registerForm.ActionUrl = emailRegisterPath

	err := form.ParseAndValidate(r, registerForm)
	if err != nil {
		return a.registrationFormError(w, r, registerForm, "Unable to process form data", err)
	}

	if !a.Policy.RegistrationAvailable(w, r) || !a.Policy.Admit(w, r, registerForm.Email, a.contentManager) {
		return
	}
	userMessage, registrationErr := auth_services.ProcessRegistration(r.Context(), a.accounts, registerForm.Email, registerForm.Password, AuthTypeEmail, a.config.Auth.RegisterRequiresVerification)
	if registrationErr != nil {
		logger.Audit(r.Context(), "registration", "failed")
		return a.registrationFormError(w, r, registerForm, userMessage, registrationErr)
	}

	account, err := a.accounts.GetByEmail(r.Context(), registerForm.Email)
	if err != nil {
		a.contentManager.HandleError(w, r, "Your account was created, but its status could not be loaded. Try signing in.", err, http.StatusServiceUnavailable)
		return
	}
	logger.Audit(r.Context(), "registration", "succeeded")
	if account.NeedsVerification() {
		// The account already exists. Offer sign-in instead of another registration.
		registerForm.Password, registerForm.PasswordMatch = "", ""
		register := a.views["auth.partial.email.registration.pending"].Content(r, registerForm)
		const deliveryFailure = "Your account was created, but we could not send the verification email. You can sign in to request another email. Protected pages remain unavailable until you verify your address."
		origin, err := util.BuildPublicURL(r, a.config.HTTP, "", nil)
		if err == nil {
			err = a.sendRegistrationVerification(r.Context(), registerForm.Email, *origin)
		}
		if err != nil {
			form.HandleFormError(w, r, register, registerForm, deliveryFailure, errors.New("verification email delivery failed"))
			return
		}
	}

	successMessage := "You have been successfully registered"
	if account.NeedsVerification() {
		successMessage = "You have been successfully registered. Check your email for verification instructions."
	}
	if err := flash.AddSuccessNotification(w, r, successMessage); err != nil {
		logger.LogRequestError(r, errors.New("registration succeeded but its notification could not be saved"))
	}
	util.RedirectToURL(w, r, a.contentManager.AuthURL)
	return result
}

func (a *EmailAuthService) registrationFormError(w http.ResponseWriter, r *http.Request, f *RegisterForm, message string, err error) auth_services.RegistrationResult {
	var parseErr *form.ParseError
	if errors.As(err, &parseErr) {
		util.HttpError(w, r, parseErr.Error(), nil, parseErr.StatusCode)
		return auth_services.RegistrationResult{}
	}
	var fields form.FieldErrors
	if !errors.As(err, &fields) {
		logger.LogRequestError(r, err)
		f.AddErrorMessage(message)
	}
	markup, err := a.renderRegistrationForm(r, f)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to display the registration form.", err, http.StatusInternalServerError)
		return auth_services.RegistrationResult{}
	}
	return auth_services.RegistrationResult{Form: markup}
}
