// Package auth implements the email/password authentication provider.
// Importing this package registers an EmailAuthService with the parent auth package.
// The provider uses the application's account repository, content manager, and mail client.
// Bundled templates are embedded and do not depend on the process working directory.
package auth

import (
	"bytes"
	"context"
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
	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/routing"
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
		TemplateFS        fs.FS
		accounts          user.AccountRepository
		sessions          *session.SessionManager
		config            *config.Config
		contentManager    *content_services.ContentManagerService
		templates         map[string]*content.Templates
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

// Init initializes the EmailAuthService instance
func (a *EmailAuthService) Init(s *server.ApplicationServer) error {
	configOptions := auth_services.GetAuthConfigOptions(s.Config, AuthTypeEmail)
	authServiceEnabled := strings.EqualFold(configOptions["enabled"], "true") || configOptions["enabled"] == "1"
	if !authServiceEnabled {
		return auth_services.NewErrEmailAuthServiceInit(errors.New("the email auth service is not enabled"))
	}

	if a.accounts == nil {
		return auth_services.NewErrEmailAuthServiceInit(errors.New("the account repository is not configured"))
	}
	if err := flash.ValidateSessionManager(a.sessions); err != nil {
		return fmt.Errorf("initialize email authentication: %w", err)
	}

	a.config = s.Config
	a.contentManager = s.ContentManager

	if a.config.Auth.JwtKey == "" {
		return auth_services.NewErrEmailAuthServiceInit(errors.New("the auth JWT key is not configured"))
	}

	var err error
	if a.TemplateFS == nil {
		a.TemplateFS = emailAuthTemplatesFS
	}
	a.verificationEmail, err = template.ParseFS(a.TemplateFS, emailVerificationTemplate)
	if err != nil {
		return auth_services.NewErrEmailAuthServiceInit(fmt.Errorf("load verification email template %s: %w", emailVerificationTemplate, err))
	}
	a.resetEmail, err = template.ParseFS(a.TemplateFS, emailResetTemplate)
	if err != nil {
		return auth_services.NewErrEmailAuthServiceInit(fmt.Errorf("load reset email template %s: %w", emailResetTemplate, err))
	}

	loginButtonTemplate := emailLoginButtonTemplateName
	a.loginButton, err = util.LoadHTML(a.TemplateFS, loginButtonTemplate)
	if err != nil {
		return auth_services.NewErrEmailAuthServiceInit(fmt.Errorf("could not find %s", loginButtonTemplate))
	}

	registerButtonTemplate := emailRegisterButtonTemplateName
	a.registerButton, err = util.LoadHTML(a.TemplateFS, registerButtonTemplate)
	if err != nil {
		return auth_services.NewErrEmailAuthServiceInit(fmt.Errorf("could not find %s", registerButtonTemplate))
	}

	return a.prepareTemplates()
}

func (a *EmailAuthService) prepareTemplates() error {
	prepared := make(map[string]*content.Templates)
	for _, name := range []string{
		emailLoginFormTemplate,
		emailRegisterFormTemplate,
		emailResetRequestFormTemplate,
		emailResetPasswordFormTemplate,
		emailChangePasswordFormTemplate,
		"templates/html/partials/registration-pending.html",
	} {
		templates, err := content.PrepareTemplates(a.contentManager, content.TemplatePath{FS: a.TemplateFS, Path: name})
		if err != nil {
			return fmt.Errorf("prepare email auth template %s: %w", name, err)
		}
		prepared[name] = templates
	}
	a.templates = prepared
	return nil
}

// Routes is implemented to fulfill the AuthService interface but is not supported by
// the EmailAuthService implementation
func (a *EmailAuthService) Routes(mux *routing.Routes) error {
	// none needed for EmailAuthService
	return nil
}

// IsLoaded currently always returns true. It does not verify initialization,
// configuration, or dependency availability.
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
	if !a.config.Auth.Enabled || !a.config.Auth.RegistrationEnabled {
		return ""
	}
	return a.registerButton
}

// GetLogin serves the login form
func (a *EmailAuthService) GetLogin(w http.ResponseWriter, r *http.Request) {
	login := content.NewManagedContent(r, a.contentManager)
	login.PartialName = emailLoginFormPartial
	login.Templates = a.templates[emailLoginFormTemplate]
	loginForm := &LoginForm{}
	loginForm.ActionUrl = emailLoginPath

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	loginForm.SetMessages(authMessages)

	login.Data = loginForm

	err = login.Render(w, r)
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
	login.PartialName = emailLoginFormPartial
	login.Templates = a.templates[emailLoginFormTemplate]
	loginForm := &LoginForm{}
	loginForm.ActionUrl = emailLoginPath

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
	u, err := a.accounts.GetByEmail(r.Context(), loginForm.Email)
	var notFound *user.ErrUserNotFound
	if err != nil && !errors.As(err, &notFound) {
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
	if !a.config.Auth.Enabled || !a.config.Auth.RegistrationEnabled {
		auth_services.RegistrationUnavailable(w, r)
		return
	}

	register := content.NewManagedContent(r, a.contentManager)
	register.PartialName = emailRegisterFormPartial
	register.Templates = a.templates[emailRegisterFormTemplate]
	registerForm := &RegisterForm{}
	registerForm.ActionUrl = emailRegisterPath

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	registerForm.SetMessages(authMessages)

	register.Data = registerForm

	err = register.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(w, r,
			"Unable to display the email authorization service registration form",
			err,
			http.StatusInternalServerError)
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

	register := content.NewManagedContent(r, a.contentManager)
	register.PartialName = emailRegisterFormPartial
	register.Templates = a.templates[emailRegisterFormTemplate]
	registerForm := &RegisterForm{}
	registerForm.ActionUrl = emailRegisterPath

	errMessage, err := form.ParseAndValidate(r, registerForm)
	if err != nil {
		form.HandleFormError(w, r, register, registerForm, errMessage, err)
		return
	}

	if registerForm.HasErrors() {
		form.HandleFormError(w, r, register, registerForm, "", nil)
		return
	}

	userMessage, registrationErr := auth_services.ProcessRegistration(r.Context(), a.accounts, registerForm.Email, registerForm.Password, AuthTypeEmail, a.config.Auth.RegisterRequiresVerification)
	if registrationErr != nil {
		form.HandleFormError(w, r, register, registerForm, userMessage, registrationErr)
		return
	}

	if a.config.Auth.RegisterRequiresVerification {
		// The account already exists. Offer sign-in instead of another registration.
		register.PartialName = "auth.partial.email.registration.pending"
		register.Templates = a.templates["templates/html/partials/registration-pending.html"]
		const deliveryFailure = "Your account was created, but we could not send the verification email. You can sign in to request another email. Protected pages remain unavailable until you verify your address."
		createdUser, err := a.accounts.GetByEmail(r.Context(), registerForm.Email)
		if err != nil || createdUser == nil {
			form.HandleFormError(w, r, register, registerForm, deliveryFailure, errors.New("verification account lookup failed"))
			return
		}

		origin, err := util.BuildPublicURL(r, a.config.HTTP, "", nil)
		if err == nil {
			err = a.SendVerificationEmail(r.Context(), createdUser, *origin)
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
		logger.LogRequestError(r, err)
	}
	util.RedirectToURL(w, r, a.contentManager.AuthURL)
}

// ValidateVerification checks delivery configuration without attempting delivery.
func (a *EmailAuthService) ValidateVerification() error {
	return a.mailClient.ValidateConfig()
}

// SendVerificationEmail serves both registration and authenticated resend requests.
func (a *EmailAuthService) SendVerificationEmail(ctx context.Context, u *user.User, origin url.URL) error {
	if a.mailClient == nil {
		return messaging.ErrMailUnavailable
	}

	if u == nil || u.ID == "" {
		return errors.New("user not specified for verification email")
	}

	verificationEndpoint := a.config.Auth.VerificationEndpoint
	if verificationEndpoint == "" {
		verificationEndpoint = emailVerifyPath
	}

	tokenExpiration := a.config.Auth.VerificationTokenExpiration
	if tokenExpiration <= 0 {
		tokenExpiration = 24 * time.Hour
	}

	verificationToken, err := user.NewAuthVerificationToken(u, []byte(a.config.Auth.JwtKey), tokenExpiration)
	if err != nil {
		return err
	}

	verificationURL := url.URL{Scheme: origin.Scheme, Host: origin.Host, Path: verificationEndpoint}
	verificationURL.RawQuery = url.Values{"token": {verificationToken.Token}}.Encode()

	body, err := a.renderAccountEmail(a.verificationEmail, verificationURL.String())
	if err != nil {
		return err
	}

	if err := a.accounts.InsertToken(ctx, verificationToken.Metadata()); err != nil {
		return err
	}
	return a.mailClient.Compose().
		To(u.Email).
		Subject("Verify your account").
		Body(body).
		Send(ctx)
}

func (a *EmailAuthService) renderAccountEmail(t *template.Template, link string) (string, error) {
	var body bytes.Buffer
	if err := t.Execute(&body, struct{ Name, URL string }{
		Name: a.config.App.Name, URL: link,
	}); err != nil {
		return "", err
	}

	return body.String(), nil
}

func (a *EmailAuthService) sendResetEmail(ctx context.Context, u *user.User, expiration time.Duration, link url.URL) error {
	if a.mailClient == nil {
		return messaging.ErrMailUnavailable
	}

	token, err := user.NewAuthResetToken(u, []byte(a.config.Auth.JwtKey), expiration)
	if err != nil {
		return err
	}
	query := link.Query()
	query.Set("token", token.Token)
	link.RawQuery = query.Encode()

	body, err := a.renderAccountEmail(a.resetEmail, link.String())
	if err != nil {
		return err
	}

	if err := a.accounts.InsertToken(ctx, token.Metadata()); err != nil {
		return err
	}
	return a.mailClient.Compose().
		To(u.Email).
		Subject("Reset your password").
		Body(body).
		Send(ctx)
}

// GetResetRequest serves the password reset request page with the resetPasswordRequestForm form
func (a *EmailAuthService) GetResetRequest(w http.ResponseWriter, r *http.Request) {
	reset := content.NewManagedContent(r, a.contentManager)
	reset.PartialName = emailResetRequestFormPartial
	reset.Templates = a.templates[emailResetRequestFormTemplate]

	resetRequestForm := &ResetPasswordRequestForm{}
	resetRequestForm.ActionUrl = emailResetRequestPath

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	resetRequestForm.SetMessages(authMessages)

	reset.Data = resetRequestForm

	err = reset.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(w, r,
			"Unable to display the email authorization service password reset request form",
			err,
			http.StatusInternalServerError)
		return
	}
}

// ResetRequest uses the same acknowledgment for eligible, unknown, and failed requests.
// The acknowledgment confirms receipt of the request, not delivery of an email.
// Valid requests have a minimum response time, not an additional processing timeout.
// Delivery and rendering errors do not change the completed storage operations.
func (a *EmailAuthService) ResetRequest(w http.ResponseWriter, r *http.Request, tokenExpiration time.Duration) {
	respondAfter := time.Now().Add(a.config.Auth.ResetMinimumResponseTime)
	reset := content.NewManagedContent(r, a.contentManager)
	reset.PartialName = emailResetRequestFormPartial
	reset.Templates = a.templates[emailResetRequestFormTemplate]

	resetRequestForm := &ResetPasswordRequestForm{}
	resetRequestForm.ActionUrl = emailResetRequestPath

	errMessage, err := form.ParseAndValidate(r, resetRequestForm)
	if err != nil {
		form.HandleFormError(w, r, reset, resetRequestForm, errMessage, err)
		return
	}

	if resetRequestForm.HasErrors() {
		form.HandleFormError(w, r, reset, resetRequestForm, "", nil)
		return
	}

	u, err := a.accounts.GetByEmail(r.Context(), resetRequestForm.Email)
	var notFound *user.ErrUserNotFound
	if errors.As(err, &notFound) {
		err = nil
	}
	if err == nil && u != nil && u.RegistrationAuthType == AuthTypeEmail {
		var link *url.URL
		link, err = util.BuildPublicURL(r, a.config.HTTP, emailResetPath, nil)
		if err == nil {
			err = a.sendResetEmail(r.Context(), u, tokenExpiration, *link)
		}
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
		a.contentManager.HandleError(
			w,
			r,
			"Unable to display the email authorization service reset request form",
			err,
			http.StatusInternalServerError)
	}
}

// GetReset serves the password reset page with the resetPasswordForm form
func (a *EmailAuthService) GetReset(w http.ResponseWriter, r *http.Request, token string) {
	resetErrMsg := "Unable to display the email authorization service password reset form"

	if token == "" {
		a.contentManager.HandleError(w, r, resetErrMsg, user.NewErrTokenNotSpecified(nil), http.StatusInternalServerError)
		return
	}

	resetForm := &ResetPasswordForm{}
	resetForm.ActionUrl = emailResetPath + "?" + url.Values{"token": {token}}.Encode()

	reset := content.NewManagedContent(r, a.contentManager)
	reset.PartialName = emailResetPasswordFormPartial
	reset.Templates = a.templates[emailResetPasswordFormTemplate]

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	resetForm.SetMessages(authMessages)

	reset.Data = resetForm

	err = reset.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(w, r, resetErrMsg, err, http.StatusInternalServerError)
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
	reset := content.NewManagedContent(r, a.contentManager)
	reset.PartialName = emailResetPasswordFormPartial
	reset.Templates = a.templates[emailResetPasswordFormTemplate]

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
		return
	}

	resetForm.ActionUrl = emailResetPath + "?" + url.Values{"token": {token}}.Encode()

	errMessage, err := form.ParseAndValidate(r, resetForm)
	if err != nil {
		form.HandleFormError(w, r, reset, resetForm, errMessage, err)
		return
	}

	if resetForm.HasErrors() {
		form.HandleFormError(w, r, reset, resetForm, "", nil)
		return
	}

	if requireNewCredentials {
		if u.Password != "" && password.CheckPasswordHash(resetForm.Password, u.Password) {
			// the new password is the same as the old, but a new password is required according
			// to the configuration
			form.HandleFormError(w, r,
				reset,
				resetForm,
				"The provided password has been recently used. For security reasons, a new password is required.",
				nil)
			return
		}
	}

	genericResetErrMsg := "Unable to reset password"

	hashedPassword, err := password.HashPassword(resetForm.Password)
	if err != nil {
		// the password could not be hashed
		form.HandleFormError(w, r, reset, resetForm, genericResetErrMsg, err)
		return
	}

	err = u.ResetPassword(r.Context(), a.accounts, token, []byte(a.config.Auth.JwtKey), hashedPassword)
	if err != nil {
		// the user could not be updated
		form.HandleFormError(
			w,
			r,
			reset,
			resetForm,
			genericResetErrMsg,
			fmt.Errorf("error updating a user's password during reset: %w", err))
		return
	}

	*r = *r.WithContext(user.ClearUserFromContext(r.Context()))
	if err := flash.AddSuccessNotification(w, r, "Your password has been updated. Login to access the site."); err != nil {
		logger.LogRequestError(r, err)
	}
	util.RedirectToURL(w, r, a.contentManager.AuthURL)
}

// GetChange serves the password change page with the changePasswordForm form
func (a *EmailAuthService) GetChange(w http.ResponseWriter, r *http.Request) {
	changeErrMsg := "Unable to display the email authorization service password change form"

	changeForm := &ChangePasswordForm{}
	changeForm.ActionUrl = emailChangePath

	change := content.NewManagedContent(r, a.contentManager)
	change.PartialName = emailChangePasswordFormPartial
	change.Templates = a.templates[emailChangePasswordFormTemplate]

	authMessages, err := flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages", err, http.StatusInternalServerError)
		return
	}
	changeForm.SetMessages(authMessages)

	change.Data = changeForm

	err = change.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(w, r, changeErrMsg, err, http.StatusInternalServerError)
		return
	}
}

// Change authenticates the specified user and, if authentication succeeds and a new
// password meets the password complexity criteria, changes the user's password to a new value.
func (a *EmailAuthService) Change(w http.ResponseWriter, r *http.Request, u *user.User) {
	changeErrMsg := "unable to change your password"

	change := content.NewManagedContent(r, a.contentManager)
	change.PartialName = emailChangePasswordFormPartial
	change.Templates = a.templates[emailChangePasswordFormTemplate]

	changeForm := &ChangePasswordForm{}
	change.Data = changeForm
	changeForm.ActionUrl = emailChangePath

	if u == nil {
		form.HandleFormError(
			w,
			r,
			change,
			changeForm,
			"Unable to change your password due to an internal error",
			user.NewErrUserNotSpecified(fmt.Errorf("change failed")))
		return
	}

	errMessage, err := form.ParseAndValidate(r, changeForm)
	if err != nil {
		form.HandleFormError(w, r, change, changeForm, errMessage, err)
		return
	}

	if changeForm.HasErrors() {
		form.HandleFormError(w, r, change, changeForm, "", nil)
		return
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
		return
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
		return
	}

	err = a.accounts.ChangePassword(r.Context(), u, hashedPassword)
	if err != nil {
		// the user could not be updated
		form.HandleFormError(
			w,
			r,
			change,
			changeForm,
			changeErrMsg,
			fmt.Errorf("Unable to save the updated password: %w", err))
		return
	}

	changeForm.OldPassword = ""
	changeForm.NewPassword = ""
	changeForm.NewPasswordMatch = ""
	*r = *r.WithContext(user.ClearUserFromContext(r.Context()))
	identity.ClearAuthenticationCookie(w, r)
	changeForm.AddSuccessMessage("Your password has been changed. Please log in again.")

	err = change.Render(w, r)
	if err != nil {
		a.contentManager.HandleError(
			w,
			r,
			"The password was updated, but there was an error displaying the change form.",
			err,
			http.StatusInternalServerError)
	}
}
