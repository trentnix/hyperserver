package auth

import (
	"errors"
	"net/http"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// ExternalAccountRequest describes an already-validated external identity and
// the intent captured in one-use, browser-bound flow state. Never construct the
// intent from callback query parameters. Mode must be login, register, or link.
type ExternalAccountRequest struct {
	Identity       user.ExternalIdentity
	Mode           string
	AccountID      string
	SessionVersion int64
	// VerifiedEmail is optional for login and linking. For registration, it must
	// be an address whose current ownership the provider has established, not
	// merely an email-shaped username or an untrusted profile attribute.
	VerifiedEmail string
}

// ExternalAccounts applies local account rules after a provider validates its
// protocol response. It does not verify tokens or create browser sessions.
// The caller supplies application-owned services and the provider identifier.
type ExternalAccounts struct {
	Identities          user.ExternalIdentityRepository
	Policy              Policy
	Content             *contentservice.ContentManagerService
	Provider            string
	RegistrationEnabled bool
}

// Resolve returns the account and whether it was just created. The provider can
// return either account to the manager to establish a session. A nil account means
// a rejection was written. Existing emails never cause automatic identity linking.
func (a ExternalAccounts) Resolve(w http.ResponseWriter, r *http.Request, request ExternalAccountRequest) (*user.User, bool) {
	fail := func(message string, err error, status int) (*user.User, bool) {
		logger.Audit(r.Context(), "external_account", "rejected")
		a.Content.HandleError(w, r, message, err, status)
		return nil, false
	}
	if request.Identity.Issuer == "" || request.Identity.Subject == "" ||
		(request.Mode != "login" && request.Mode != "register" && request.Mode != "link") {
		return fail("Sign-in could not be verified. Please start again.", nil, http.StatusBadRequest)
	}
	current := user.GetUserFromContext(r.Context())
	if request.Mode == "link" {
		if current == nil || current.ID != request.AccountID || current.SessionVersion != request.SessionVersion || current.NeedsVerification() {
			return fail("Your login changed. Sign in again before connecting another login method.", nil, http.StatusForbidden)
		}
		if !a.Policy.Admit(w, r, current.Email, a.Content) {
			return nil, false
		}
		// Storage checks the captured account version again in the transaction.
		if err := a.Identities.LinkExternalIdentity(r.Context(), current, request.Identity); err != nil {
			if errors.Is(err, user.ErrIdentityConflict) || errors.Is(err, user.ErrUserChanged) {
				return fail("This login method could not be connected. Your login may have changed or the identity may already be connected.", nil, http.StatusConflict)
			}
			return fail("Connecting this login method is temporarily unavailable. Please try again.", err, http.StatusServiceUnavailable)
		}
		return current, false
	}
	if current != nil {
		return fail("You're already logged in.", nil, http.StatusForbidden)
	}
	account, err := a.Identities.GetExternalAccount(r.Context(), request.Identity)
	if err == nil {
		if !a.Policy.Admit(w, r, account.Email, a.Content) {
			return nil, false
		}
		return account, false
	}
	if !errors.Is(err, user.ErrUserNotFound) {
		return fail("Account access is temporarily unavailable.", err, http.StatusServiceUnavailable)
	}
	if request.Mode != "register" || !a.RegistrationEnabled {
		return fail("This login method is not connected to an account. Sign in with your existing method to connect it, or register a new account.", nil, http.StatusForbidden)
	}
	if user.EmailKey(request.VerifiedEmail) == "" {
		return fail("Register and verify your email address first, then connect this login method.", nil, http.StatusForbidden)
	}
	if !a.Policy.RegistrationAvailable(w, r) || !a.Policy.Admit(w, r, request.VerifiedEmail, a.Content) {
		return nil, false
	}
	account = &user.User{Email: request.VerifiedEmail, Verified: true, VerificationRequired: true, RegistrationAuthType: a.Provider}
	err = a.Identities.CreateExternalAccount(r.Context(), account, request.Identity)
	if errors.Is(err, user.ErrEmailAlreadyExists) {
		if invitations, ok := a.Identities.(user.InvitationRepository); ok {
			err = invitations.RegisterInvited(r.Context(), account, nil, &request.Identity)
		}
	}
	if err != nil {
		if errors.Is(err, user.ErrEmailAlreadyExists) || errors.Is(err, user.ErrIdentityConflict) {
			return fail("An account already exists. Sign in with your existing method to connect this login method.", nil, http.StatusConflict)
		}
		return fail("Registration is temporarily unavailable. Please try again.", err, http.StatusServiceUnavailable)
	}
	return account, true
}
