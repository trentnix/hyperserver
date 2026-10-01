package user

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
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
	authToken, err := newAuthToken(u, jwtKey, expiration, resetTokenType)
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

// ResetPassword consumes the exact reset token and stores passwordHash in one transaction.
// The caller must validate and hash the new password before calling. The account
// schema must exist. A concurrent password or provider change rejects the operation.
// The receiver changes only after commit. Other account fields and tokens are unchanged.
func (u *User) ResetPassword(ctx context.Context, db *sqlx.DB, token string, jwtKey []byte, passwordHash string) error {
	if u == nil || u.ID == "" {
		return NewErrUserNotSpecified(nil)
	}
	if db == nil {
		return database.NewErrDatabaseUnavailable(nil)
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

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := consumeAuthToken(ctx, tx, token, claims); err != nil {
		return err
	}
	now := time.Now()

	// Only update the password. Do not overwrite unrelated account changes with
	// the snapshot used for form validation or the previous-password check.
	result, err := tx.ExecContext(ctx, `UPDATE `+userTableName+` SET password = ?, updated_at = ?
		WHERE id = ? AND password = ? AND registration_auth_type = ?`,
		passwordHash, now, u.ID, u.Password, u.RegistrationAuthType)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return NewErrToken(errors.New("account changed or no longer exists"))
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	u.Password = passwordHash
	u.UpdatedAt = now
	return nil
}
