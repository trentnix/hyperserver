package user

import (
	"context"
	"errors"
)

var (
	ErrLastAdministrator = errors.New("Keep at least one active admin.")
	ErrAccessDenied      = errors.New("You don't have permission to manage users.")
	ErrAlreadyActive     = errors.New("This user already has site access.")
	ErrRemoveSelf        = errors.New("You can't remove your own access.")
)

// AdministrationRepository is optional account administration. The application
// defines role names and supplies its administrator role. Mutations must recheck
// the actor's current, verified, accepted access inside the write transaction.
// Role changes use SessionVersion and revoke sessions. Empty roles remove access
// without deleting accounts or content. The last accepted administrator is protected.
type AdministrationRepository interface {
	ListUsers(context.Context) ([]User, error)
	// InitializeRole assigns the initial role only when no account has any role.
	// It never creates an account or replaces an assigned role.
	InitializeRole(ctx context.Context, email, role string) error
	// InviteUser inserts a credential-free account or reinvites a removed/pending
	// account. Existing credentials and verification requirements stay unchanged.
	// u carries the expected version.
	// The invitation replaces all earlier invitation tokens atomically.
	InviteUser(ctx context.Context, actorID, administratorRole string, u *User, token TokenMetadata) error
	// ChangeUserRole returns ErrUserNotFound for accounts without a role.
	// Restore removed access through InviteUser.
	// Removing the actor's own access returns ErrRemoveSelf.
	ChangeUserRole(ctx context.Context, actorID, administratorRole string, u *User) error
}

// InvitationRepository completes invitations without changing registered
// credentials. Callers must validate/hash passwords and authenticate external
// identities. All operations must be atomic and preserve inputs on failure.
type InvitationRepository interface {
	// RegisterInvited accepts only credential-free invited accounts. A valid
	// invitation token or verified external identity proves email ownership.
	// Otherwise email registration follows u.VerificationRequired before accepting access.
	RegisterInvited(ctx context.Context, u *User, token *TokenMetadata, identity *ExternalIdentity) error
	// AcceptInvitation requires an authenticated account snapshot. A verified
	// account, or one that does not require verification, can accept without a token.
	// Otherwise a valid email-bound token is required. Only a valid token proves
	// email ownership. Storage rechecks the version, pending flag, recipient, and expiry.
	AcceptInvitation(ctx context.Context, u *User, token *TokenMetadata) error
}
