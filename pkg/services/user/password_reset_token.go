// password_reset_token.go handles the creation and management of a token that can be
// used to verify a user attempting to reset user authorization.
package user

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
)

type (
	// AuthResetToken defines the data that will be serialized to the database to
	// manage a reset token
	AuthResetToken struct {
		UserId    string `db:"user_id"`
		TokenHash string `db:"token_hash"`
		Token     string
		ExpiresAt time.Time `db:"expires_at"`
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

// GetAuthResetTokenByID retrieves an AuthResetToken for the specified user from the database
func GetAuthResetTokenByID(db *sqlx.DB, userId string) (*AuthResetToken, error) {
	if userId == "" {
		return nil, NewErrInvalidResetToken(fmt.Errorf("get token by user: user id in not specified"))
	}

	var token AuthResetToken

	query := fmt.Sprintf(`
        SELECT user_id, token_hash, expires_at
        FROM %s
        WHERE user_id = ? AND token_type = ?
    `, userTokenTableName)

	err := db.Get(&token, query, userId, resetTokenType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No row was found, handle accordingly.
			return nil, NewErrInvalidResetToken(fmt.Errorf("no reset token found for the given user"))
		}
		return nil, database.NewErrDatabase(fmt.Errorf("error retrieving a reset token by user: %w", err))
	}

	return &token, nil
}

// GetAuthResetTokenByHash retrieves an AuthResetToken for the specified token value from the database
func GetAuthResetTokenByHash(db *sqlx.DB, token string) (*AuthResetToken, error) {
	if token == "" {
		return nil, NewErrInvalidResetToken(fmt.Errorf("get token by value: token value in not specified"))
	}

	var passwordResetToken AuthResetToken

	query := fmt.Sprintf(`
        SELECT user_id, token_hash, expires_at
        FROM %s
        WHERE token_hash = ?
    `, userTokenTableName)

	err := db.Get(&passwordResetToken, query, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// no row was found, handle accordingly.
			return nil, NewErrInvalidResetToken(fmt.Errorf("no reset token found for the given value"))
		}
		return nil, database.NewErrDatabase(fmt.Errorf("error retrieving a reset token by value: %w", err))
	}

	return &passwordResetToken, nil
}

// Create inserts an AuthRequestToken into the database
func (token *AuthResetToken) Create(db *sqlx.DB) error {
	if token.UserId == "" {
		return NewErrInvalidResetToken(fmt.Errorf("create token: user id in not specified"))
	}

	if token.TokenHash == "" {
		return NewErrInvalidResetToken(fmt.Errorf("create token: token value in not specified"))
	}

	if token.ExpiresAt.IsZero() {
		return NewErrInvalidResetToken(fmt.Errorf("create token: expiration date in not specified"))
	}

	query := fmt.Sprintf(`
        INSERT INTO %s (user_id, token_hash, expires_at, token_type)
        VALUES (:user_id, :token_hash, :expires_at, '%s')
    `, userTokenTableName, resetTokenType)

	_, err := db.NamedExec(query, token)
	return err
}

// Delete removes an AuthResetToken from the database
func (token *AuthResetToken) Delete(db *sqlx.DB) error {
	if token.UserId == "" {
		return NewErrInvalidResetToken(fmt.Errorf("delete token: user id in not specified"))
	}

	if token.TokenHash == "" {
		return NewErrInvalidResetToken(fmt.Errorf("delete token: token value in not specified"))
	}

	query := fmt.Sprintf(`
        DELETE FROM %s
        WHERE user_id = ? AND token_hash = ?
    `, userTokenTableName)

	results, err := db.Exec(query, token.UserId, token.TokenHash)
	if err != nil {
		return err
	}

	rowsAffected, err := results.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("error deleting the specified token from the database: no rows affected")
	}

	return nil
}

// DeleteAllTokensByUser deletes all of the tokens from the database for the specified user
func DeleteAllTokensByUser(db *sqlx.DB, userId string) error {
	if userId == "" {
		return NewErrInvalidResetToken(fmt.Errorf("delete all user tokens: user id in not specified"))
	}

	query := fmt.Sprintf(`
        DELETE FROM %s
        WHERE user_id = ?
    `, userTokenTableName)

	_, err := db.Exec(query, userId)
	return err
}

// NewPasswordResetToken creates a password reset authorization token, stores it in the
// database as a hashed value, and returns the token to the caller
func NewPasswordResetToken(hs_user *User, jwtKey []byte, expiration time.Duration) (*AuthResetToken, error) {
	if hs_user == nil {
		return nil, NewErrInvalidResetToken(fmt.Errorf("new token creation: user id in not specified"))
	}

	if expiration <= 0 {
		return nil, NewErrInvalidResetToken(fmt.Errorf("new token creation: expiration date in not specified"))
	}

	// create token - user.id and expiration
	expirationTime := time.Now().Add(expiration)

	claims := &VerificationClaims{
		Id: hs_user.ID,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
		},
	}

	// Create and sign the token with the specified algorithm and claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	resetToken, err := token.SignedString(jwtKey)
	if err != nil {
		return nil, NewErrToken(err)
	}

	hash := sha256.Sum256([]byte(resetToken))
	resetTokenHash := hex.EncodeToString(hash[:])

	passwordResetToken := &AuthResetToken{
		UserId:    hs_user.ID,
		TokenHash: resetTokenHash,
		Token:     resetToken,
		ExpiresAt: expirationTime,
	}

	// return token
	return passwordResetToken, nil
}

// ValidateResetToken confirms whether the provided reset authorization token is valid
func ValidateResetToken(db *sqlx.DB, tokenString string) (*User, error) {
	// hash the received token
	hash := sha256.Sum256([]byte(tokenString))
	tokenHash := hex.EncodeToString(hash[:])

	// retrieve the token record from the database
	passwordResetToken, err := GetAuthResetTokenByHash(db, tokenHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, NewErrTokenNotFound(err)
		}

		return nil, err
	}

	// check if the token is expired
	if time.Now().After(passwordResetToken.ExpiresAt) {
		return nil, NewErrTokenExpired(fmt.Errorf("reset token validation failed"))
	}

	// retrieve the associated user
	user, err := GetUserByID(db, passwordResetToken.UserId)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, NewErrUserNotFound(err)
		}

		return nil, database.NewErrDatabase(err)
	}

	return user, nil
}
