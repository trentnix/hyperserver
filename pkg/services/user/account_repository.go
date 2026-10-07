package user

import (
	"context"
	"time"
)

// AccountRepository stores accounts for authentication. Reads of missing accounts
// must return ErrUserNotFound. Other failures must remain errors. Implementations
// must honor cancellation and support concurrent calls. Successful reads must
// return independent, non-nil User values.
type AccountRepository interface {
	GetByID(context.Context, string) (*User, error)
	GetByEmail(context.Context, string) (*User, error)
	// GetToken reads metadata matching the token hash and purpose without consuming it.
	// Missing tokens return ErrTokenNotFound. Expired tokens remain readable so the
	// caller can distinguish expiry from absence. Other failures must remain errors.
	GetToken(ctx context.Context, tokenHash, purpose string) (TokenMetadata, error)
	// Create inserts only. Duplicate emails return database.ErrRecordAlreadyExists.
	// Failure must leave stored accounts and u unchanged. Success fills in the ID,
	// timestamps, and initial SessionVersion of 1. Password is already hashed.
	Create(context.Context, *User) error
	// CreateToken inserts only. The account ID and token hash identify the record.
	// Duplicate records must return an error without replacing the stored token.
	// Failure must leave storage unchanged. Success must not change other tokens or accounts.
	// Token generation, signing, and delivery belong to the caller.
	CreateToken(context.Context, TokenMetadata) error
	// Update changes an existing account by ID. Missing accounts return ErrUserNotFound,
	// stale SessionVersion values return ErrUserChanged, and duplicate emails return
	// database.ErrRecordAlreadyExists. The caller must preserve the version from its
	// account read and validate and hash any new password before calling.
	// Changes to Password, Email, RegistrationAuthType, Verified, or VerificationRequired
	// must increment SessionVersion. Otherwise, the version must stay unchanged.
	// ID and CreatedAt must stay unchanged. An email change must also revoke the account's
	// verification tokens. These changes must commit together and preserve other tokens.
	// Success updates u's UpdatedAt and SessionVersion. Failure must leave u and storage unchanged.
	Update(context.Context, *User) error
	// ChangePassword atomically replaces the password hash and increments SessionVersion
	// only if the stored password, registration provider, and SessionVersion match u.
	// A missing account or a mismatch must return an error. Failure must leave storage
	// and u unchanged. Success updates u's Password, UpdatedAt, and SessionVersion.
	// The caller must validate and hash the new password before calling.
	ChangePassword(ctx context.Context, u *User, passwordHash string) error
	// ResetPassword atomically consumes the exact reset token, replaces the password,
	// and increments SessionVersion. The caller must validate the token's signature,
	// reset purpose, and account identity, and validate and hash the new password.
	// Implementations must match the stored token's hash, account, and reset purpose
	// and recheck both signed and stored expiry when consuming it, including after
	// any wait for storage.
	// A missing token returns ErrTokenNotFound. Expiry returns ErrTokenExpired.
	// A missing account or changed password or provider must reject the operation.
	// Success refreshes u from storage. Failure must leave u and storage unchanged.
	// Other account fields and tokens must remain unchanged.
	ResetPassword(ctx context.Context, u *User, authorization ResetAuthorization, passwordHash string) error
	// Verify atomically consumes the exact verification token and marks its account
	// verified only if the current email matches the signed email. The caller must
	// validate the token's signature, verification purpose, account ID, and email.
	// Implementations must match the stored token's hash, account, and verification
	// purpose and recheck signed and stored expiry when consuming it, including after
	// any wait for storage. Missing tokens return ErrTokenNotFound. Expiry returns ErrTokenExpired.
	// Success updates UpdatedAt and increments SessionVersion only if the account
	// was not already verified. It returns the committed account. Other fields and
	// tokens must stay unchanged. Failure must return nil and leave storage unchanged.
	Verify(context.Context, VerificationAuthorization) (*User, error)
}

// TokenMetadata contains only the fields needed to store an issued account token.
// AccountID, TokenHash, and ExpiresAt must be set. Purpose must be "auth-reset" or
// "auth-verification". TokenHash is the lowercase hexadecimal SHA-256 hash of the
// signed token. The raw token and signing key must not be passed to storage.
type TokenMetadata struct {
	AccountID string
	TokenHash string
	Purpose   string
	ExpiresAt time.Time
}

// ResetAuthorization carries the storage checks for a signature-validated reset token.
// ExpiresAt is the signed expiry, not the stored expiry. Construct this value only
// after validating the token's signature, reset purpose, and account identity.
type ResetAuthorization struct {
	AccountID string
	TokenHash string
	ExpiresAt time.Time
}

// VerificationAuthorization carries the storage checks for a signature-validated
// verification token. Email and ExpiresAt must come from its signed claims.
// Construct this value only after validating the signature and verification purpose.
type VerificationAuthorization struct {
	AccountID string
	Email     string
	TokenHash string
	ExpiresAt time.Time
}
