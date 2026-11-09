package user

import (
	"context"
	"errors"
	"time"
)

// AuthVerificationToken contains a verification token and its storage metadata.
type AuthVerificationToken struct{ AuthToken }

// NewAuthVerificationToken creates a distinct token bound to u.ID and u.Email.
// Store its Metadata before sending the link. Other tokens remain valid.
func NewAuthVerificationToken(u *User, expiration time.Duration) (*AuthVerificationToken, error) {
	token, err := newAuthToken(u, expiration, VerificationTokenPurpose)
	if err != nil {
		return nil, err
	}
	return &AuthVerificationToken{AuthToken: *token}, nil
}

// ValidateVerificationToken checks stored metadata and current email without consuming it.
func ValidateVerificationToken(ctx context.Context, accounts AccountRepository, raw string) (*User, error) {
	return validateToken(ctx, accounts, raw, VerificationTokenPurpose)
}

// Verify consumes the token and verifies its recipient email atomically.
// A changed email address rejects the token. Failures leave account and token unchanged.
func Verify(ctx context.Context, accounts AccountRepository, raw string) (*User, error) {
	repository, ok := accounts.(VerificationRepository)
	if !ok {
		return nil, errors.New("account storage does not support email verification")
	}
	token, err := loadToken(ctx, accounts, raw, VerificationTokenPurpose)
	if err != nil {
		return nil, err
	}
	return repository.Verify(ctx, VerificationAuthorization{
		AccountID: token.AccountID, Email: token.Email,
		TokenHash: token.TokenHash, ExpiresAt: token.ExpiresAt,
	})
}
