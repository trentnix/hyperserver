package user

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
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

// NewAuthVerificationToken creates a verification token and its hash. Call Create to store it.
func NewAuthVerificationToken(u *User, jwtKey []byte, expiration time.Duration) (*AuthVerificationToken, error) {
	authToken, err := newAuthToken(u.ID, jwtKey, expiration, verificationTokenType)
	if err != nil {
		return nil, err
	}

	return &AuthVerificationToken{AuthToken: *authToken}, nil
}

// ValidateVerificationToken checks a signed verification token against its stored
// hash, purpose, and expiry. It does not consume the token. Unlike Verify, it requires a stored record.
func ValidateVerificationToken(db *sqlx.DB, tokenString string, jwtKey []byte) (*User, error) {
	return validateToken(db, tokenString, jwtKey, verificationTokenType)
}

// Verify checks the signed verification token and marks its account verified.
// It does not require or consume a stored token. Reuse is allowed until expiry.
func Verify(ctx context.Context, db *sqlx.DB, verificationToken string, jwtKey []byte) (*User, error) {
	claims, err := parseAuthToken(verificationToken, jwtKey, verificationTokenType)
	if err != nil {
		return nil, err
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
