// Package auth implements the email/password authentication provider.
// Importing this package registers an EmailAuthService with the parent auth package.
// The provider uses the application's account repository, content manager, and mail client.
// Bundled templates are embedded and do not depend on the process working directory.
package auth

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/auth/identity"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
	"github.com/trentnix/hyperserver/pkg/server"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

type (
	// EmailAuthService implements email/password authentication using shared application services.
	EmailAuthService struct {
		// TemplateFS overrides bundled templates. Set before Init. Nil uses embedded assets.
		// Reads during Init are synchronous and cannot be interrupted by a context.
		TemplateFS        fs.FS
		accounts          user.AccountRepository
		sessions          *session.SessionManager
		config            *config.Config
		contentManager    *content_services.ContentManagerService
		views             map[string]*content.View
		mailClient        *messaging.MailClient
		verificationEmail *template.Template
		resetEmail        *template.Template

		loginButton    template.HTML
		registerButton template.HTML
	}

	// LoginForm defines the fields used when logging in via email/password
	LoginForm struct {
		Email    string
		Password string

		form.Form
	}

	// RegisterForm binds an email address and matching passwords for account creation.
	RegisterForm struct {
		Email         string
		Password      string
		PasswordMatch string

		form.Form
	}

	// ResetPasswordRequestForm defines the fields used when requesting to reset a user's password
	ResetPasswordRequestForm struct {
		Email string

		form.Form
	}

	// ResetPasswordForm defines the fields used when resetting the password
	ResetPasswordForm struct {
		Password      string
		PasswordMatch string

		form.Form
	}

	// ChangePasswordForm binds the current password and matching replacement passwords.
	ChangePasswordForm struct {
		OldPassword      string
		NewPassword      string
		NewPasswordMatch string

		form.Form
	}
)

// Bind populates the login email address and password from r.
func (lf *LoginForm) Bind(r *http.Request) error {
	lf.Email = r.FormValue("email")
	lf.Password = r.FormValue("password")
	return nil
}

// Bind populates the registration email, password, and confirmation from r.
func (rf *RegisterForm) Bind(r *http.Request) error {
	rf.Email = r.FormValue("email")
	rf.Password = r.FormValue("password")
	rf.PasswordMatch = r.FormValue("passwordMatch")
	return nil
}

// Bind populates the reset-request email address from r.
func (rprf *ResetPasswordRequestForm) Bind(r *http.Request) error {
	rprf.Email = r.FormValue("email")
	return nil
}

// Bind populates the replacement password and confirmation from r.
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
	// AuthTypeEmail identifies the email/password provider in routes and configuration.
	AuthTypeEmail = "email"

	emailLoginFormTemplate          = "templates/html/partials/login-form.html"
	emailLoginButtonTemplateName    = "templates/html/partials/login-link.html"
	emailRegisterFormTemplate       = "templates/html/partials/register-form.html"
	emailRegisterButtonTemplateName = "templates/html/partials/register-link.html"
	emailResetRequestFormTemplate   = "templates/html/partials/reset-request.html"
	emailResetPasswordFormTemplate  = "templates/html/partials/reset-password.html"
	emailChangePasswordFormTemplate = "templates/html/partials/change-password.html"
	emailVerificationTemplate       = "templates/email/verification.html"
	emailResetTemplate              = "templates/email/reset.html"
	emailLoginFormPartial           = "auth.partial.email.login.form"
	emailRegisterFormPartial        = "auth.partial.email.register.form"
	emailResetRequestFormPartial    = "auth.partial.email.reset.request.form"
	emailResetPasswordFormPartial   = "auth.partial.email.reset.password.form"
	emailChangePasswordFormPartial  = "auth.partial.email.change.password.form"

	emailLoginPath        = "/auth/login/email"
	emailRegisterPath     = "/auth/register/email"
	emailVerifyPath       = "/auth/verify"
	emailResetRequestPath = "/auth/reset/request/email"
	emailResetPath        = "/auth/reset/email"
	emailChangePath       = "/auth/change/email"
)

// init makes the provider discoverable without creating application state.
func init() {
	auth_services.Register(Provider(nil, nil, nil))
}

// Provider supplies email authentication with initialized accounts and sessions,
// and an optional mail client. The application owns these dependencies.
// A nil client leaves delivery unavailable. Registration that requires verification
// rejects unavailable delivery during module initialization.
func Provider(accounts user.AccountRepository, sessions *session.SessionManager, mail *messaging.MailClient) auth_services.Descriptor {
	return auth_services.Descriptor{Name: AuthTypeEmail, New: func() auth_services.AuthService {
		return &EmailAuthService{accounts: accounts, sessions: sessions, mailClient: mail}
	}}
}

// Init validates dependencies and settings and prepares email and page templates.
// Provider options accept only enabled. Other auth settings belong under auth.
// It does not access account or session storage, send mail, or acquire resources
// that need shutdown. Supplied services remain owned by application setup.
func (a *EmailAuthService) Init(s *server.ApplicationServer) error {
	configOptions := auth_services.GetAuthConfigOptions(s.Config, AuthTypeEmail)
	for name := range configOptions {
		if name != "enabled" {
			return fmt.Errorf("%w: auth.services.email accepts only the enabled option", auth_services.ErrEmailAuthServiceInit)
		}
	}
	authServiceEnabled := strings.EqualFold(configOptions["enabled"], "true") || configOptions["enabled"] == "1"
	if !authServiceEnabled {
		return hs_errors.WithCause(auth_services.ErrEmailAuthServiceInit, errors.New("the email auth service is not enabled"))
	}

	if a.accounts == nil {
		return hs_errors.WithCause(auth_services.ErrEmailAuthServiceInit, errors.New("the account repository is not configured"))
	}
	if err := flash.ValidateSessionManager(a.sessions); err != nil {
		return fmt.Errorf("initialize email authentication: %w", err)
	}

	if err := config.ValidateSigningKey("auth.jwtKey", s.Config.Auth.JwtKey); err != nil {
		return hs_errors.WithCause(auth_services.ErrEmailAuthServiceInit, err)
	}

	a.config = s.Config
	a.contentManager = s.ContentManager

	var err error
	if a.TemplateFS == nil {
		a.TemplateFS = emailAuthTemplatesFS
	}
	a.verificationEmail, err = template.ParseFS(a.TemplateFS, emailVerificationTemplate)
	if err != nil {
		return hs_errors.WithCause(auth_services.ErrEmailAuthServiceInit, fmt.Errorf("load verification email template %s: %w", emailVerificationTemplate, err))
	}
	a.resetEmail, err = template.ParseFS(a.TemplateFS, emailResetTemplate)
	if err != nil {
		return hs_errors.WithCause(auth_services.ErrEmailAuthServiceInit, fmt.Errorf("load reset email template %s: %w", emailResetTemplate, err))
	}

	loginButtonTemplate := emailLoginButtonTemplateName
	a.loginButton, err = util.LoadHTML(a.TemplateFS, loginButtonTemplate)
	if err != nil {
		return hs_errors.WithCause(auth_services.ErrEmailAuthServiceInit, fmt.Errorf("could not find %s", loginButtonTemplate))
	}

	registerButtonTemplate := emailRegisterButtonTemplateName
	a.registerButton, err = util.LoadHTML(a.TemplateFS, registerButtonTemplate)
	if err != nil {
		return hs_errors.WithCause(auth_services.ErrEmailAuthServiceInit, fmt.Errorf("could not find %s", registerButtonTemplate))
	}

	return a.prepareTemplates()
}

func (a *EmailAuthService) prepareTemplates() error {
	prepared := make(map[string]*content.View)
	for _, page := range []struct{ file, partial string }{
		{emailLoginFormTemplate, emailLoginFormPartial},
		{emailRegisterFormTemplate, emailRegisterFormPartial},
		{emailResetRequestFormTemplate, emailResetRequestFormPartial},
		{emailResetPasswordFormTemplate, emailResetPasswordFormPartial},
		{emailChangePasswordFormTemplate, emailChangePasswordFormPartial},
		{"templates/html/partials/registration-pending.html", "auth.partial.email.registration.pending"},
	} {
		view, err := content.PrepareView(a.contentManager, page.partial, content.TemplatePath{FS: a.TemplateFS, Path: page.file})
		if err != nil {
			return fmt.Errorf("prepare email auth template %s: %w", page.file, err)
		}
		prepared[page.partial] = view
	}
	a.views = prepared
	return nil
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
	if !a.config.Auth.Enabled || !a.config.Auth.RegistrationEnabled {
		return ""
	}
	return a.registerButton
}

// GetLogin serves the login form
func (a *EmailAuthService) GetLogin(w http.ResponseWriter, r *http.Request) {
	loginForm := &LoginForm{}
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
func (a *EmailAuthService) Login(w http.ResponseWriter, r *http.Request) *user.User {
	loginForm := &LoginForm{}
	loginForm.ActionUrl = emailLoginPath
	login := a.views[emailLoginFormPartial].Content(r, loginForm)

	err := form.ParseAndValidate(r, loginForm)
	if err != nil {
		form.HandleFormError(w, r, login, loginForm, "Unable to process form data", err)
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

// GetRegister serves the register form
func (a *EmailAuthService) GetRegister(w http.ResponseWriter, r *http.Request) {
	if !a.config.Auth.Enabled || !a.config.Auth.RegistrationEnabled {
		auth_services.RegistrationUnavailable(w, r)
		return
	}

	registerForm := &RegisterForm{}
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

	registerForm := &RegisterForm{}
	registerForm.ActionUrl = emailRegisterPath
	register := a.views[emailRegisterFormPartial].Content(r, registerForm)

	err := form.ParseAndValidate(r, registerForm)
	if err != nil {
		form.HandleFormError(w, r, register, registerForm, "Unable to process form data", err)
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

// ValidateVerification checks delivery configuration without attempting delivery.
func (a *EmailAuthService) ValidateVerification() error {
	return a.mailClient.ValidateConfig()
}

// GetResetRequest serves the password reset request page with the resetPasswordRequestForm form
func (a *EmailAuthService) GetResetRequest(w http.ResponseWriter, r *http.Request) {
	resetRequestForm := &ResetPasswordRequestForm{}
	resetRequestForm.ActionUrl = emailResetRequestPath
	reset := a.views[emailResetRequestFormPartial].Content(r, resetRequestForm)

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	resetRequestForm.SetMessages(authMessages)

	err = reset.Render(w, r)
	if err != nil {
		reset.HandleRenderError(w, r,
			"Unable to display the email authorization service password reset request form",
			err)
		return
	}
}

// ResetRequest uses the same acknowledgment for eligible, unknown, and failed requests.
// The acknowledgment confirms receipt of the request, not delivery of an email.
// Valid requests have a minimum response time, not an additional processing timeout.
// Delivery and rendering errors do not change the completed storage operations.
func (a *EmailAuthService) ResetRequest(w http.ResponseWriter, r *http.Request, tokenExpiration time.Duration) {
	respondAfter := time.Now().Add(a.config.Auth.ResetMinimumResponseTime)
	resetRequestForm := &ResetPasswordRequestForm{}
	resetRequestForm.ActionUrl = emailResetRequestPath
	reset := a.views[emailResetRequestFormPartial].Content(r, resetRequestForm)

	err := form.ParseAndValidate(r, resetRequestForm)
	if err != nil {
		form.HandleFormError(w, r, reset, resetRequestForm, "Unable to process form data", err)
		return
	}

	link, err := util.BuildPublicURL(r, a.config.HTTP, emailResetPath, nil)
	if err == nil {
		err = a.requestPasswordReset(r.Context(), resetRequestForm.Email, tokenExpiration, *link)
	}
	if err != nil {
		// Provider errors can contain the token or email body. Log only the failure.
		if l := logger.Get(r.Context()); l != nil {
			l.Error("password reset instructions could not be sent")
		} else {
			log.Print("password reset instructions could not be sent")
		}
	}

	if err := waitForResetResponse(r.Context(), respondAfter); err != nil {
		return
	}

	resetRequestForm.Email = ""
	resetRequestForm.AddMessage("Password reset request received. If the account is eligible, check your email for instructions. If no email arrives, try again later.")
	reset.Data = resetRequestForm
	err = reset.Render(w, r)
	if err != nil {
		reset.HandleRenderError(
			w,
			r,
			"Unable to display the email authorization service reset request form",
			err)
	}
}

// GetReset serves the password reset page with the resetPasswordForm form
func (a *EmailAuthService) GetReset(w http.ResponseWriter, r *http.Request, token string) {
	resetErrMsg := "Unable to display the email authorization service password reset form"

	if token == "" {
		a.contentManager.HandleError(w, r, "No reset token specified. Please request a new link.", nil, http.StatusBadRequest)
		return
	}

	resetForm := &ResetPasswordForm{}
	resetForm.ActionUrl = emailResetPath + "?" + url.Values{"token": {token}}.Encode()

	reset := a.views[emailResetPasswordFormPartial].Content(r, resetForm)

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	resetForm.SetMessages(authMessages)

	err = reset.Render(w, r)
	if err != nil {
		reset.HandleRenderError(w, r, resetErrMsg, err)
		return
	}
}

// Reset validates the password form and atomically redeems the reset token.
func (a *EmailAuthService) Reset(
	w http.ResponseWriter,
	r *http.Request,
	u *user.User,
	token string,
	requireNewCredentials bool,
) {
	resetForm := &ResetPasswordForm{}
	reset := a.views[emailResetPasswordFormPartial].Content(r, resetForm)

	if u == nil {
		a.contentManager.HandleError(w, r, "Unable to reset password. Please try again later.",
			errors.New("password reset requires an account"), http.StatusInternalServerError)
		return
	}

	resetForm.ActionUrl = emailResetPath + "?" + url.Values{"token": {token}}.Encode()

	err := form.ParseAndValidate(r, resetForm)
	if err != nil {
		form.HandleFormError(w, r, reset, resetForm, "Unable to process form data", err)
		return
	}

	err = a.resetPassword(r.Context(), u, token, resetForm.Password, requireNewCredentials)
	if err != nil {
		logger.Audit(r.Context(), "password_reset", "failed")
	} else {
		logger.Audit(r.Context(), "password_reset", "succeeded")
	}
	if errors.Is(err, errPasswordReused) {
		form.HandleFormError(w, r, reset, resetForm,
			"The provided password has been recently used. For security reasons, a new password is required.", nil)
		return
	}
	if err != nil {
		if user.IsTokenRejection(err) {
			a.contentManager.HandleError(w, r,
				"Unable to reset password: the link is invalid or expired. Please request a new link.", nil, http.StatusBadRequest)
			return
		}
		a.contentManager.HandleError(w, r, "Unable to reset password. Please try again later.",
			errors.New("password reset operation failed"), http.StatusInternalServerError)
		return
	}

	*r = *r.WithContext(user.ClearUserFromContext(r.Context()))
	if err := flash.AddSuccessNotification(w, r, "Your password has been updated. Login to access the site."); err != nil {
		logger.LogRequestError(r, errors.New("password reset succeeded but its notification could not be saved"))
	}
	util.RedirectToURL(w, r, a.contentManager.AuthURL)
}

// GetChange serves the password change page with the changePasswordForm form
func (a *EmailAuthService) GetChange(w http.ResponseWriter, r *http.Request) {
	changeErrMsg := "Unable to display the email authorization service password change form"

	changeForm := &ChangePasswordForm{}
	changeForm.ActionUrl = emailChangePath

	change := a.views[emailChangePasswordFormPartial].Content(r, changeForm)

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	changeForm.SetMessages(authMessages)

	err = change.Render(w, r)
	if err != nil {
		change.HandleRenderError(w, r, changeErrMsg, err)
		return
	}
}

// Change authenticates the specified user and, if authentication succeeds and a new
// password meets the password complexity criteria, changes the user's password to a new value.
func (a *EmailAuthService) Change(w http.ResponseWriter, r *http.Request, u *user.User) {
	changeErrMsg := "unable to change your password"

	changeForm := &ChangePasswordForm{}
	changeForm.ActionUrl = emailChangePath
	change := a.views[emailChangePasswordFormPartial].Content(r, changeForm)

	if u == nil {
		form.HandleFormError(
			w,
			r,
			change,
			changeForm,
			"Unable to change your password due to an internal error",
			hs_errors.WithCause(user.ErrUserNotSpecified, fmt.Errorf("change failed")))
		return
	}

	err := form.ParseAndValidate(r, changeForm)
	if err != nil {
		form.HandleFormError(w, r, change, changeForm, "Unable to process form data", err)
		return
	}

	err = a.changePassword(r.Context(), u, changeForm.OldPassword, changeForm.NewPassword)
	if err != nil {
		logger.Audit(r.Context(), "password_change", "failed")
	} else {
		logger.Audit(r.Context(), "password_change", "succeeded")
	}
	if errors.Is(err, errInvalidCredentials) {
		form.HandleFormError(w, r, change, changeForm, "The provided password is incorrect", nil)
		return
	}
	if err != nil {
		form.HandleFormError(w, r, change, changeForm, changeErrMsg, err)
		return
	}

	changeForm.OldPassword = ""
	changeForm.NewPassword = ""
	changeForm.NewPasswordMatch = ""
	*r = *r.WithContext(user.ClearUserFromContext(r.Context()))
	identity.ClearAuthenticationCookie(w, r)
	changeForm.AddSuccessMessage("Your password has been changed. Please log in again.")

	change = a.views[emailChangePasswordFormPartial].Content(r, changeForm)
	err = change.Render(w, r)
	if err != nil {
		change.HandleRenderError(
			w,
			r,
			"The password was updated, but there was an error displaying the change form.",
			err)
	}
}
