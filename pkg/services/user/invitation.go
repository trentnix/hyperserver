package user

import (
	"context"
	"time"
)

const InvitationTokenPurpose = "auth-invitation"

type invitationKey struct{}

// WithInvitation carries the link token through the application's registration
// handler. It is not authorization until storage validates and consumes it.
func WithInvitation(ctx context.Context, raw string) context.Context {
	return context.WithValue(ctx, invitationKey{}, raw)
}

func NewInvitationToken(u *User, expiration time.Duration) (*AuthToken, error) {
	return newAuthToken(u, expiration, InvitationTokenPurpose)
}

// RegistrationInvitation validates a supplied link, including its recipient.
// No link returns nil so ordinary registration can request email verification.
func RegistrationInvitation(ctx context.Context, accounts AccountRepository, email string) (*TokenMetadata, error) {
	raw, _ := ctx.Value(invitationKey{}).(string)
	if raw == "" {
		return nil, nil
	}
	token, err := loadToken(ctx, accounts, raw, InvitationTokenPurpose)
	if err != nil {
		return nil, err
	}
	if email != "" && EmailKey(token.Email) != EmailKey(email) {
		return nil, ErrToken
	}
	return &token, nil
}

func ValidateInvitation(ctx context.Context, accounts AccountRepository, raw string) (*User, error) {
	return validateToken(ctx, accounts, raw, InvitationTokenPurpose)
}
