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
		UserId string `db:"user_id"`
		// A token hash is used because the hash is stored in the database. That ensures
		// that if the database is compromised, the tokens themselves won't be
		// compromised. It's similar to storing a password hash.
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

// GetAuthResetTokenByUser retrieves an AuthResetToken for the specified user from the database
func GetAuthResetTokenByUser(db *sqlx.DB, userId string) (*AuthResetToken, error) {
	if userId == "" {
		return nil, NewErrUserNotSpecified(fmt.Errorf("get token by user"))
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
			return nil, NewErrTokenNotFound(fmt.Errorf("get token by user: %s", userId))
		}
		return nil, database.NewErrDatabase(fmt.Errorf("get token by user: %s: %w", userId, err))
	}

	return &token, nil
}

// GetAuthResetTokenByHash retrieves an AuthResetToken for the specified token value from the database
func GetAuthResetTokenByHash(db *sqlx.DB, token string) (*AuthResetToken, error) {
	if token == "" {
		return nil, NewErrTokenNotSpecified(fmt.Errorf("get token by value"))
	}

	var passwordResetToken AuthResetToken

	query := fmt.Sprintf(`
        SELECT user_id, token_hash, expires_at
        FROM %s
        WHERE token_hash = ? AND token_type = ?
    `, userTokenTableName)

	err := db.Get(&passwordResetToken, query, token, resetTokenType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// no row was found, handle accordingly.
			return nil, NewErrTokenNotFound(fmt.Errorf("get token by value"))
		}
		return nil, database.NewErrDatabase(fmt.Errorf("get token by value: %w", err))
	}

	return &passwordResetToken, nil
}

// Create inserts an AuthRequestToken into the database
func (token *AuthResetToken) Create(db *sqlx.DB) error {
	errCreateToken := errors.New("error creating a new token in the database")
	if token.UserId == "" {
		return NewErrUserNotSpecified(errCreateToken)
	}

	if token.TokenHash == "" {
		return NewErrTokenNotSpecified(errCreateToken)
	}

	if token.ExpiresAt.IsZero() {
		return NewErrTokenExpirationNotSpecified(errCreateToken)
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
	errDeleteToken := errors.New("error deleting an existing token")
	if token.UserId == "" {
		return NewErrUserNotSpecified(errDeleteToken)
	}

	if token.TokenHash == "" {
		return NewErrTokenNotSpecified(errDeleteToken)
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
		return NewErrTokenNotFound(errDeleteToken)
	}

	return nil
}

// DeleteAllTokensByUser deletes all of the tokens from the database for the specified user
func DeleteAllTokensByUser(db *sqlx.DB, userId string) error {
	if userId == "" {
		return NewErrUserNotSpecified(errors.New("error deleting all user tokens"))
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
func NewPasswordResetToken(u *User, jwtKey []byte, expiration time.Duration) (*AuthResetToken, error) {
	errNewResetToken := errors.New("error creating a new reset token instance")
	if u == nil {
		return nil, NewErrUserNotSpecified(errNewResetToken)
	}

	if expiration <= 0 {
		return nil, NewErrTokenExpirationNotSpecified(errNewResetToken)
	}

	// create token - user.id and expiration
	expirationTime := time.Now().Add(expiration)

	claims := &VerificationClaims{
		Id: u.ID,
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
		UserId:    u.ID,
		TokenHash: resetTokenHash,
		Token:     resetToken,
		ExpiresAt: expirationTime,
	}

	// return token
	return passwordResetToken, nil
}

// ValidateResetToken confirms whether the provided reset authorization token is valid
func ValidateResetToken(db *sqlx.DB, tokenString string) (*User, error) {
	errValidateReset := errors.New("error validating the specified reset token")
	if tokenString == "" {
		return nil, NewErrTokenNotSpecified(errValidateReset)
	}

	// hash the received token
	hash := sha256.Sum256([]byte(tokenString))
	tokenHash := hex.EncodeToString(hash[:])

	// retrieve the token record from the database
	passwordResetToken, err := GetAuthResetTokenByHash(db, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, NewErrTokenNotFound(err)
		}

		return nil, err
	}

	// check if the token is expired
	if time.Now().After(passwordResetToken.ExpiresAt) {
		return nil, NewErrTokenExpired(errValidateReset)
	}

	// retrieve the associated user
	user, err := GetUserByID(db, passwordResetToken.UserId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, NewErrUserNotFound(err)
		}

		return nil, database.NewErrDatabase(err)
	}

	return user, nil
}
