package blog

import (
	"net/http"

	emailauth "github.com/trentnix/hyperserver/auth/modules/email"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// EmailProvider keeps account admission in the blog. Embedded email authentication
// still owns credentials, forms, verification, recovery, and password changes.
type EmailProvider struct {
	*emailauth.EmailAuthService
	access  *Access
	content *contentservice.ContentManagerService
}

// NewEmailProvider restricts registration and login to the configured administrator.
// Dependencies remain owned by the application. Access is initialized by the blog
// module before any request can reach the provider.
func NewEmailProvider(accounts user.AccountRepository, sessions *session.SessionManager, mail *messaging.MailClient, access *Access) *EmailProvider {
	email := emailauth.Provider(accounts, sessions, mail).New().(*emailauth.EmailAuthService)
	return &EmailProvider{EmailAuthService: email, access: access}
}

// Init prepares the framework's email forms in the blog's layout.
func (p *EmailProvider) Init(app *server.ApplicationServer) error {
	p.content = app.ContentManager
	return p.EmailAuthService.Init(app)
}

// Register checks admission before the email provider can create an account.
func (p *EmailProvider) Register(w http.ResponseWriter, r *http.Request) {
	if p.admit(w, r) {
		p.EmailAuthService.Register(w, r)
	}
}

// Login checks admission before checking credentials or creating a session.
func (p *EmailProvider) Login(w http.ResponseWriter, r *http.Request) *user.User {
	if !p.admit(w, r) {
		return nil
	}
	return p.EmailAuthService.Login(w, r)
}

// admit checks the submitted email against the administrator rule.
// It writes an error response when parsing, admission, or access setup fails.
func (p *EmailProvider) admit(w http.ResponseWriter, r *http.Request) bool {
	if err := form.Parse(r); err != nil {
		p.content.HandleError(w, r, err.Message, nil, err.StatusCode)
		return false
	}
	// Match the exact value the embedded provider reads, including form precedence.
	allowed, err := p.access.AllowAccount(r.Context(), r.Form.Get("email"))
	if err != nil {
		p.content.HandleError(w, r, "Account access is unavailable.", err, http.StatusInternalServerError)
		return false
	}
	if !allowed {
		p.content.HandleError(w, r, "Registration and login are available only to the blog administrator.", nil, http.StatusForbidden)
	}
	return allowed
}
