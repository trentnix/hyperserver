package auth

import (
	"context"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Dependencies supplies application-owned services and policy to provider factories.
// Factories must not initialize or close these services.
type Dependencies struct {
	Accounts user.AccountRepository
	Sessions *session.SessionManager
	Mail     *messaging.MailClient
	Policy   Policy
}

// Policy applies application rules independently of the credential provider.
// Configure callbacks before startup. They must support concurrent requests.
// Nil callbacks impose no additional restrictions or behavior.
type Policy struct {
	// LoginAccount optionally limits displayed login choices to methods available
	// to this account. Return nil to show all configured methods. This presentation
	// policy does not restrict provider routes or replace account admission.
	LoginAccount func(context.Context) (*user.User, error)
	// AllowAccount checks account admission for login, registration, and linking.
	// Providers must check admission before creating accounts or linking identities.
	// The manager also checks the local account before establishing a login session.
	AllowAccount func(context.Context, string) (bool, error)
	// BeforeRegister checks availability before displaying registration or creating
	// an account. Return false after writing a response to stop the operation.
	BeforeRegister func(http.ResponseWriter, *http.Request) bool
	// AfterLogin runs after the authenticated session is saved, before redirecting.
	// It may set cookies but must not write a response body or commit headers.
	AfterLogin func(http.ResponseWriter, *http.Request, *user.User)
}

// Admit checks the email against application policy and renders a failure when
// denied or unavailable. Providers call it before registration and account linking.
func (p Policy) Admit(w http.ResponseWriter, r *http.Request, email string, content *content.ContentManagerService) bool {
	if p.AllowAccount == nil {
		return true
	}
	allowed, err := p.AllowAccount(r.Context(), email)
	if err != nil {
		logger.Audit(r.Context(), "account_admission", "failed")
		content.HandleError(w, r, "Account access is temporarily unavailable.", err, http.StatusServiceUnavailable)
		return false
	}
	if !allowed {
		logger.Audit(r.Context(), "account_admission", "rejected")
		content.HandleError(w, r, "This account is not allowed to access this site.", nil, http.StatusForbidden)
	}
	return allowed
}

// RegistrationAvailable lets an application close registration independently of
// the configured provider. Providers must recheck asynchronous registrations.
func (p Policy) RegistrationAvailable(w http.ResponseWriter, r *http.Request) bool {
	return p.BeforeRegister == nil || p.BeforeRegister(w, r)
}
