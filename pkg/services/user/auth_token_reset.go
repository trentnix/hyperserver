package user

import (
	"context"
	"errors"
	"time"
)

// AuthResetToken contains a password-reset token and its storage metadata.
type AuthResetToken struct{ AuthToken }

// NewAuthResetToken creates a reset token and its hash.
// Store its Metadata with TokenRepository.InsertToken before sending the link.
func NewAuthResetToken(u *User, expiration time.Duration) (*AuthResetToken, error) {
	token, err := newAuthToken(u, expiration, ResetTokenPurpose)
	if err != nil {
		return nil, err
	}
	return &AuthResetToken{AuthToken: *token}, nil
}

// ValidateResetToken checks the stored hash, purpose, and expiry without consuming it.
func ValidateResetToken(ctx context.Context, accounts AccountRepository, raw string) (*User, error) {
	return validateToken(ctx, accounts, raw, ResetTokenPurpose)
}

// ResetPassword consumes the token and stores passwordHash atomically.
// The caller must validate and hash the new password. Success refreshes u and
// invalidates existing authenticated sessions. A concurrent credential change rejects it.
func (u *User) ResetPassword(ctx context.Context, accounts AccountRepository, raw, passwordHash string) error {
	if u == nil || u.ID == "" {
		return ErrUserNotSpecified
	}
	if passwordHash == "" {
		return errors.New("a password hash is required")
	}
	repository, ok := accounts.(PasswordResetRepository)
	if !ok {
		return errors.New("account storage does not support password reset")
	}
	token, err := loadToken(ctx, accounts, raw, ResetTokenPurpose)
	if err != nil {
		return err
	}
	if token.AccountID != u.ID {
		return ErrToken
	}
	return repository.ResetPassword(ctx, u, ResetAuthorization{
		AccountID: token.AccountID, TokenHash: token.TokenHash, ExpiresAt: token.ExpiresAt,
	}, passwordHash)
}
