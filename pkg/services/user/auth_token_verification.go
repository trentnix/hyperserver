// password_reset_token.go handles the creation and management of a token that can be
// used to verify a user attempting to reset user authorization.
package user

import (
	"time"

	"github.com/jmoiron/sqlx"
)

type (
	// AuthResetToken defines the data that will be serialized to the database to
	// manage a reset token
	AuthVerificationToken struct {
		AuthToken
	}
)

const (
	verificationTokenType = "auth-verification"
)

// GetAuthResetTokenByUser retrieves an AuthResetToken for the specified user from the database
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

// GetAuthResetTokenByHash retrieves an AuthResetToken for the specified token value from the database
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

// NewPasswordResetToken creates a password reset authorization token, stores it in the
// database as a hashed value, and returns the token to the caller
func NewAuthVerificationToken(u *User, jwtKey []byte, expiration time.Duration) (*AuthVerificationToken, error) {
	authToken, err := newAuthToken(u.ID, jwtKey, expiration, verificationTokenType)
	if err != nil {
		return nil, err
	}

	return &AuthVerificationToken{AuthToken: *authToken}, nil
}

// ValidateVerificationToken confirms whether the provided reset authorization token is valid
func ValidateVerificationToken(db *sqlx.DB, tokenString string) (*User, error) {
	return validateToken(db, tokenString, verificationTokenType)
}
