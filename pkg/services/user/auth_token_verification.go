// password_reset_token.go handles the creation and management of a token that can be
// used to verify a user attempting to reset user authorization.
package user

import (
	"context"
	"fmt"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
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

// Verify handles a verification request by extracting and processing the provided token, updating
// the user to verified, and returning the newly verified user
func Verify(ctx context.Context, db *sqlx.DB, verificationToken string, jwtKey []byte) (*User, error) {
	// Parse the token with the specified claims and signing method
	claims := &VerificationClaims{}
	token, err := jwt.ParseWithClaims(verificationToken, claims, func(token *jwt.Token) (interface{}, error) {
		// Verify the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, NewErrToken(fmt.Errorf("unexpected signing method: %v", token.Header["alg"]))
		}

		return jwtKey, nil
	})
	// If there's an error, or the token is invalid, return false
	if err != nil {
		return nil, NewErrToken(fmt.Errorf("could not parse the JWT: %w", err))
	}

	if !token.Valid {
		return nil, NewErrToken(fmt.Errorf("the verification token could not be parsed."))
	}

	userAccount, err := GetUserByID(db, claims.Id)
	if err != nil {
		return nil, NewErrUserNotFound(err)
	}

	alreadyVerified := userAccount.Verified
	if !alreadyVerified {
		userAccount.Verified = true
		err := userAccount.Update(ctx, db)
		if err != nil {
			return nil, database.NewErrDatabase(fmt.Errorf("error updating the specified user in the database: %w", err))
		}
	}

	return userAccount, nil
}
