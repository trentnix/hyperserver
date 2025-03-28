// auth_reset_token.go handles the creation and management of a token that can be
// used to verify a user attempting to reset user authorization.
package user

import (
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/jmoiron/sqlx"
)

type (
	// AuthResetToken provides a container for a reset AuthToken
	AuthResetToken struct {
		AuthToken
	}

	// Claims contains the data that is serialized to and from a JWT
	VerificationClaims struct {
		Id string
		jwt.StandardClaims
	}
)

const (
	resetTokenType = "auth-reset"
)

// GetAuthResetTokenByUser retrieves an AuthResetToken for the specified user from the database
func GetAuthResetTokenByUser(db *sqlx.DB, userId string) (*AuthResetToken, error) {
	authToken, err := getTokenByUser(db, userId, resetTokenType)
	if err != nil {
		return nil, err
	}

	authVerificationToken := &AuthResetToken{
		AuthToken: *authToken,
	}

	return authVerificationToken, nil
}

// GetAuthResetTokenByHash retrieves an AuthResetToken for the specified token value from the database
func GetAuthResetTokenByHash(db *sqlx.DB, token string) (*AuthResetToken, error) {
	authToken, err := getTokenByHash(db, token, resetTokenType)
	if err != nil {
		return nil, err
	}

	authVerificationToken := &AuthResetToken{
		AuthToken: *authToken,
	}

	return authVerificationToken, nil
}

// NewAuthResetToken creates a reset authorization token, stores it in the database as
// a hashed value, and returns the token to the caller
func NewAuthResetToken(u *User, jwtKey []byte, expiration time.Duration) (*AuthResetToken, error) {
	authToken, err := newAuthToken(u.ID, jwtKey, expiration, verificationTokenType)
	if err != nil {
		return nil, err
	}

	return &AuthResetToken{AuthToken: *authToken}, nil
}

// ValidateResetToken confirms whether the provided reset authorization token is valid
func ValidateResetToken(db *sqlx.DB, tokenString string) (*User, error) {
	return validateToken(db, tokenString, resetTokenType)
}
