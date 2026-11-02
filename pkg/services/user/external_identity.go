package user

import (
	"context"
	"errors"
)

// ErrIdentityConflict means an external identity is already linked to an account.
var ErrIdentityConflict = errors.New("external identity is already linked")

// ExternalIdentity identifies an account in a trusted provider's namespace.
// For OpenID Connect, use the verified issuer and subject. Other protocols use
// a fixed provider namespace and authenticated stable user ID. Email and display
// names are not identity keys. Validate the identity before using storage.
type ExternalIdentity struct {
	Issuer, Subject string
}

// ExternalIdentityRepository is optional account storage for external login.
// Writes must be atomic, preserve input values on failure, honor cancellation,
// and enforce uniqueness of (issuer, subject) and (account, issuer).
type ExternalIdentityRepository interface {
	// HasExternalIdentity reports whether the account has a login at the issuer.
	HasExternalIdentity(ctx context.Context, accountID, issuer string) (bool, error)
	// GetExternalAccount returns ErrUserNotFound for an unlinked identity.
	GetExternalAccount(context.Context, ExternalIdentity) (*User, error)
	// CreateExternalAccount inserts the account and identity together. Existing
	// emails return ErrEmailAlreadyExists and must never be automatically linked.
	CreateExternalAccount(context.Context, *User, ExternalIdentity) error
	// LinkExternalIdentity requires the current account's SessionVersion and a
	// verified account. It increments SessionVersion and updates the supplied user
	// after commit. Conflicts must not overwrite links or account credentials.
	LinkExternalIdentity(context.Context, *User, ExternalIdentity) error
}
