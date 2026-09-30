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

	// VerificationClaims contains the signed account identity, purpose, and expiry.
	VerificationClaims struct {
		Id      string
		Purpose string `json:"purpose"`
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

// NewAuthResetToken creates a reset token and its hash. Call Create to store it.
func NewAuthResetToken(u *User, jwtKey []byte, expiration time.Duration) (*AuthResetToken, error) {
	authToken, err := newAuthToken(u.ID, jwtKey, expiration, resetTokenType)
	if err != nil {
		return nil, err
	}

	return &AuthResetToken{AuthToken: *authToken}, nil
}

// ValidateResetToken checks the signature, reset purpose, stored hash, and expiry,
// then returns the account. jwtKey is the account-token signing key. It does not consume the token.
func ValidateResetToken(db *sqlx.DB, tokenString string, jwtKey []byte) (*User, error) {
	return validateToken(db, tokenString, jwtKey, resetTokenType)
}
