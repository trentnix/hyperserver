package user

import (
	"context"
	"errors"
	"time"
)

// NewPasswordSetupToken creates an enrollment token bound to the account email.
func NewPasswordSetupToken(u *User, expiration time.Duration) (*AuthToken, error) {
	return newAuthToken(u, expiration, PasswordSetupTokenPurpose)
}

// ValidatePasswordSetupToken checks enrollment authorization without consuming it.
func ValidatePasswordSetupToken(ctx context.Context, accounts AccountRepository, raw string) (*User, error) {
	return validateToken(ctx, accounts, raw, PasswordSetupTokenPurpose)
}

// SetupPassword consumes enrollment authorization using optional storage support.
// The caller must authenticate u and validate and hash the password first.
func (u *User) SetupPassword(ctx context.Context, accounts AccountRepository, raw, hash string) error {
	if u == nil || u.ID == "" {
		return ErrUserNotSpecified
	}
	repository, ok := accounts.(PasswordSetupRepository)
	if !ok {
		return errors.New("password setup is not supported by account storage")
	}
	token, err := loadToken(ctx, accounts, raw, PasswordSetupTokenPurpose)
	if err != nil {
		return err
	}
	if token.AccountID != u.ID {
		return ErrToken
	}
	return repository.SetupPassword(ctx, u, token, hash)
}
