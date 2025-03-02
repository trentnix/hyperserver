// email_auth.go is a email/password authorization implementation of the AuthService interface
package auth

import (
	"net/http"

	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/server"
)

// EmailAuthService implements the AuthService interface
type (
	EmailAuthService struct{}

	// loginForm defines the fields used when logging in via email/password
	loginForm struct {
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
		// this is subject to future change
		http.Error(w, "the login form should be rendered via an HTMX request", http.StatusBadRequest)
	}

	login.AddContentTemplate(content.Template(emailLoginFormTemplate))
	login.Data = &loginForm{}

	err := login.Render(w)
	if err != nil {
		http.Error(w, "the login form won't render", http.StatusInternalServerError)
	}
}
