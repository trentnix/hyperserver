package auth

import (
	"errors"
	"html/template"
	"net/http"

	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
)

const (
	emailRegisterFormTemplate       = "templates/html/partials/register-form.html"
	emailRegisterButtonTemplateName = "templates/html/partials/register-link.html"
	emailRegisterFormPartial        = "auth.partial.email.register.form"
	emailRegisterPath               = "/auth/register/email"
)

// RegisterForm binds an email address and matching passwords for account creation.
type RegisterForm struct {
	Email         string
	Password      string
	PasswordMatch string
	Alternatives  []template.HTML

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

// GetRegisterForm renders email registration alongside other enabled providers.
func (a *EmailAuthService) GetRegisterForm(w http.ResponseWriter, r *http.Request) {
	a.GetRegister(w, r)
}

// GetRegister serves the register form.
func (a *EmailAuthService) GetRegister(w http.ResponseWriter, r *http.Request) {
	if !a.config.Auth.Enabled || !a.config.Auth.RegistrationEnabled {
		auth_services.RegistrationUnavailable(w, r)
		return
	}

	registerForm := &RegisterForm{Alternatives: auth_services.RegistrationAlternatives(r)}
	registerForm.ActionUrl = emailRegisterPath
	register := a.views[emailRegisterFormPartial].Content(r, registerForm)

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	registerForm.SetMessages(authMessages)

	err = register.Render(w, r)
	if err != nil {
		register.HandleRenderError(w, r,
			"Unable to display the email authorization service registration form",
			err)
	}
}

// Register validates input, creates an account, and sends verification instructions
// when required. It owns the response, including the success redirect. Delivery failure
// can occur after the account has been created, leaving it available for verification resend.
func (a *EmailAuthService) Register(w http.ResponseWriter, r *http.Request) {
	if !a.config.Auth.Enabled || !a.config.Auth.RegistrationEnabled {
		auth_services.RegistrationUnavailable(w, r)
		return
	}

	registerForm := &RegisterForm{Alternatives: auth_services.RegistrationAlternatives(r)}
	registerForm.ActionUrl = emailRegisterPath
	register := a.views[emailRegisterFormPartial].Content(r, registerForm)

	err := form.ParseAndValidate(r, registerForm)
	if err != nil {
		form.HandleFormError(w, r, register, registerForm, "Unable to process form data", err)
		return
	}

	if !a.Policy.RegistrationAvailable(w, r) || !a.Policy.Admit(w, r, registerForm.Email, a.contentManager) {
		return
	}
	userMessage, registrationErr := auth_services.ProcessRegistration(r.Context(), a.accounts, registerForm.Email, registerForm.Password, AuthTypeEmail, a.config.Auth.RegisterRequiresVerification)
	if registrationErr != nil {
		logger.Audit(r.Context(), "registration", "failed")
		form.HandleFormError(w, r, register, registerForm, userMessage, registrationErr)
		return
	}

	logger.Audit(r.Context(), "registration", "succeeded")
	if a.config.Auth.RegisterRequiresVerification {
		// The account already exists. Offer sign-in instead of another registration.
		register = a.views["auth.partial.email.registration.pending"].Content(r, registerForm)
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
	if a.config.Auth.RegisterRequiresVerification {
		successMessage = "You have been successfully registered. Check your email for verification instructions."
	}
	if err := flash.AddSuccessNotification(w, r, successMessage); err != nil {
		logger.LogRequestError(r, errors.New("registration succeeded but its notification could not be saved"))
	}
	util.RedirectToURL(w, r, a.contentManager.AuthURL)
}
