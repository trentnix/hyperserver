package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

type (
	// AuthVerificationToken contains a verification token and its storage metadata.
	AuthVerificationToken struct {
		AuthToken
	}
)

const (
	verificationTokenType = "auth-verification"
)

// GetAuthVerificationTokenByUser retrieves a stored verification token for the user.
func GetAuthVerificationTokenByUser(db *sqlx.DB, userId string) (*AuthVerificationToken, error) {
	authToken, err := getTokenByUser(db, userId, verificationTokenType)
	if err != nil {
		return nil, err
	}

	authVerificationToken := &AuthVerificationToken{
		AuthToken: *authToken,
	}

	return authVerificationToken, nil
}

// GetAuthVerificationTokenByHash retrieves a stored verification token by its hash.
func GetAuthVerificationTokenByHash(db *sqlx.DB, token string) (*AuthVerificationToken, error) {
	authToken, err := getTokenByHash(db, token, verificationTokenType)
	if err != nil {
		return nil, err
	}

	authVerificationToken := &AuthVerificationToken{
		AuthToken: *authToken,
	}

	return authVerificationToken, nil
}

// NewAuthVerificationToken creates a distinct token bound to u.ID and u.Email.
// Store it with CreateContext before sending the link. Other tokens remain valid.
func NewAuthVerificationToken(u *User, jwtKey []byte, expiration time.Duration) (*AuthVerificationToken, error) {
	authToken, err := newAuthToken(u, jwtKey, expiration, verificationTokenType)
	if err != nil {
		return nil, err
	}

	return &AuthVerificationToken{AuthToken: *authToken}, nil
}

// ValidateVerificationToken checks a signed verification token against its stored
// hash, purpose, expiry, and current email address. It does not consume the token.
func ValidateVerificationToken(db *sqlx.DB, tokenString string, jwtKey []byte) (*User, error) {
	return validateToken(db, tokenString, jwtKey, verificationTokenType)
}

// Verify validates the signed verification token and asks accounts to atomically
// consume it and verify its email address. A changed email address rejects the
// token. Failed operations leave both the account and token unchanged.
func Verify(ctx context.Context, accounts AccountRepository, verificationToken string, jwtKey []byte) (*User, error) {
	if accounts == nil {
		return nil, errors.New("the account repository is not configured")
	}

	claims, err := parseAuthToken(verificationToken, jwtKey, verificationTokenType)
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
