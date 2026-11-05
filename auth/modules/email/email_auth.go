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
	"strings"

	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
	"github.com/trentnix/hyperserver/pkg/server"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const (
	// AuthTypeEmail identifies the email/password provider in routes and configuration.
	AuthTypeEmail = "email"

	emailVerificationTemplate = "templates/email/verification.html"
	emailResetTemplate        = "templates/email/reset.html"
	emailSetupTemplate        = "templates/email/password-setup.html"
	emailVerifyPath           = "/auth/verify"
)

// EmailAuthService implements email/password authentication using shared application services.
type EmailAuthService struct {
	// Policy supplies application admission rules. Set before Init.
	Policy auth_services.Policy
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
	setupEmail        *template.Template

	loginButton    template.HTML
	registerButton template.HTML
}

// init makes the provider discoverable without creating application state.
func init() {
	auth_services.Register(Provider())
}

// New supplies email authentication with initialized accounts and sessions,
// and an optional mail client. The application owns these dependencies.
// A nil client leaves delivery unavailable. Registration that requires verification
// rejects unavailable delivery during module initialization.
// Call Init before serving requests.
func New(accounts user.AccountRepository, sessions *session.SessionManager, mail *messaging.MailClient) *EmailAuthService {
	return &EmailAuthService{accounts: accounts, sessions: sessions, mailClient: mail}
}

// Provider creates independent instances using application-supplied dependencies.
func Provider() auth_services.Descriptor {
	return auth_services.Descriptor{Name: AuthTypeEmail, New: func(d auth_services.Dependencies) auth_services.AuthService {
		provider := New(d.Accounts, d.Sessions, d.Mail)
		provider.Policy = d.Policy
		return provider
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
	a.setupEmail, err = template.ParseFS(a.TemplateFS, emailSetupTemplate)
	if err != nil {
		return fmt.Errorf("load password setup email: %w", err)
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
		{emailSetupFormTemplate, emailSetupFormPartial},
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

// ValidateVerification checks delivery configuration without attempting delivery.
func (a *EmailAuthService) ValidateVerification() error {
	return a.mailClient.ValidateConfig()
}
