package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type (
	// AuthResetToken provides a container for a reset AuthToken
	AuthResetToken struct {
		AuthToken
	}

	// VerificationClaims contains the signed account identity, purpose, and expiry.
	// Email binds verification tokens to their recipient address.
	VerificationClaims struct {
		Id      string
		Purpose string `json:"purpose"`
		Email   string `json:"email,omitempty"`
		jwt.RegisteredClaims
	}
)

const (
	resetTokenType = "auth-reset"
)

// NewAuthResetToken creates a reset token and its hash.
// Store its Metadata with AccountRepository.InsertToken before sending the link.
func NewAuthResetToken(u *User, jwtKey []byte, expiration time.Duration) (*AuthResetToken, error) {
	authToken, err := newAuthToken(u, jwtKey, expiration, resetTokenType)
	if err != nil {
		return nil, err
	}

	return &AuthResetToken{AuthToken: *authToken}, nil
}

// ValidateResetToken checks the signature, reset purpose, stored hash, and expiry,
// then returns the account. jwtKey is the account-token signing key. It does not consume the token.
func ValidateResetToken(ctx context.Context, accounts AccountRepository, tokenString string, jwtKey []byte) (*User, error) {
	return validateToken(ctx, accounts, tokenString, jwtKey, resetTokenType)
}

// ResetPassword validates the signed reset token and asks accounts to atomically
// consume it and store passwordHash. The caller must validate and hash the new
// password. A concurrent password or provider change rejects the operation.
// Success refreshes the receiver and invalidates existing authenticated sessions.
func (u *User) ResetPassword(ctx context.Context, accounts AccountRepository, token string, jwtKey []byte, passwordHash string) error {
	if u == nil || u.ID == "" {
		return NewErrUserNotSpecified(nil)
	}
	if accounts == nil {
		return errors.New("the account repository is not configured")
	}
	if passwordHash == "" {
		return errors.New("a password hash is required")
	}

	claims, err := parseAuthToken(token, jwtKey, resetTokenType)
	if err != nil {
		return err
	}
	if claims.Id != u.ID {
		return NewErrToken(errors.New("reset token belongs to another account"))
	}

	hash := sha256.Sum256([]byte(token))
	authorization := ResetAuthorization{
		AccountID: claims.Id,
		TokenHash: hex.EncodeToString(hash[:]),
		ExpiresAt: claims.ExpiresAt.Time,
	}
	return accounts.ResetPassword(ctx, u, authorization, passwordHash)
}
