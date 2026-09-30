package user

import (
	"context"
	"database/sql"
	"errors"
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

// Verify consumes the exact stored token and verifies its email address in one
// transaction. The account schema must exist. A changed email address rejects
// the token. Failed operations leave both the account and token unchanged.
func Verify(ctx context.Context, db *sqlx.DB, verificationToken string, jwtKey []byte) (*User, error) {
	if db == nil {
		return nil, database.NewErrDatabaseUnavailable(nil)
	}

	claims, err := parseAuthToken(verificationToken, jwtKey, verificationTokenType)
	if err != nil {
		return nil, err
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := consumeAuthToken(ctx, tx, verificationToken, claims); err != nil {
		return nil, err
	}

	var account User
	err = tx.GetContext(ctx, &account, `UPDATE `+userTableName+`
		SET verified = TRUE, updated_at = ? WHERE id = ? AND email = ?
		RETURNING id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password`,
		time.Now(), claims.Id, claims.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewErrToken(errors.New("verification address changed or account no longer exists"))
	}
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &account, nil
}
