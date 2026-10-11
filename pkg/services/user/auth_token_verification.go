package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

type (
	// AuthVerificationToken contains a verification token and its storage metadata.
	AuthVerificationToken struct {
		AuthToken
	}
)

// NewAuthVerificationToken creates a distinct token bound to u.ID and u.Email.
// Store its Metadata with AccountRepository.InsertToken before sending the link.
// Other tokens remain valid.
func NewAuthVerificationToken(u *User, jwtKey []byte, expiration time.Duration) (*AuthVerificationToken, error) {
	authToken, err := newAuthToken(u, jwtKey, expiration, VerificationTokenPurpose)
	if err != nil {
		return nil, err
	}

	return &AuthVerificationToken{AuthToken: *authToken}, nil
}

// ValidateVerificationToken checks a signed verification token against its stored
// hash, purpose, expiry, and current email address. It does not consume the token.
func ValidateVerificationToken(ctx context.Context, accounts AccountRepository, tokenString string, jwtKey []byte) (*User, error) {
	return validateToken(ctx, accounts, tokenString, jwtKey, VerificationTokenPurpose)
}

// Verify validates the signed verification token and asks accounts to atomically
// consume it and verify its email address. A changed email address rejects the
// token. Failed operations leave both the account and token unchanged.
func Verify(ctx context.Context, accounts AccountRepository, verificationToken string, jwtKey []byte) (*User, error) {
	if accounts == nil {
		return nil, errors.New("the account repository is not configured")
	}

	claims, err := parseAuthToken(verificationToken, jwtKey, VerificationTokenPurpose)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256([]byte(verificationToken))
	authorization := VerificationAuthorization{
		AccountID: claims.Id,
		Email:     claims.Email,
		TokenHash: hex.EncodeToString(hash[:]),
		ExpiresAt: claims.ExpiresAt.Time,
	}
	return accounts.Verify(ctx, authorization)
}
