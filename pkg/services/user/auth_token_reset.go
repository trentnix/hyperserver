package user

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
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

	// Claim the token with the first write so competing SQLite transactions wait
	// before reading account state. Rollback restores the token if any step fails.
	hash := sha256.Sum256([]byte(token))
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `DELETE FROM `+userTokenTableName+`
		WHERE user_id = ? AND token_hash = ? AND token_type = ?
		RETURNING expires_at`, u.ID, hex.EncodeToString(hash[:]), resetTokenType).
		Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return NewErrTokenNotFound(err)
	}
	if err != nil {
		return err
	}

	now := time.Now()
	if !now.Before(expiresAt) || now.Unix() >= claims.ExpiresAt {
		return NewErrTokenExpired(nil)
	}

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
