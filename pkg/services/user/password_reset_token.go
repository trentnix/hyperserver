// password_reset_token.go handles the creation and management of a token that can be
// used to verify a user attempting to reset user authorization.
package user

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/jmoiron/sqlx"
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
		return nil, fmt.Errorf("a user id value must be specified to retrieve a password reset token")
	}

	var token AuthResetToken

	query := fmt.Sprintf(`
        SELECT user_id, token_hash, expires_at
        FROM %s
        WHERE user_id = ? AND token_type = ?
    `, userTokenTableName)

	err := db.Get(&token, query, userId, resetTokenType)
	if err != nil {
		return nil, fmt.Errorf("error retrieving a reset token using user ID:%w", err)
	}

	return &token, nil
}

// GetAuthResetTokenByHash retrieves an AuthResetToken for the specified token value from the database
func GetAuthResetTokenByHash(db *sqlx.DB, token string) (*AuthResetToken, error) {
	if token == "" {
		return nil, fmt.Errorf("a token value must be specified to retrieve a password reset token")
	}

	var passwordResetToken AuthResetToken

	query := fmt.Sprintf(`
        SELECT user_id, token_hash, expires_at
        FROM %s
        WHERE token_hash = ?
    `, userTokenTableName)

	err := db.Get(&passwordResetToken, query, token)
	if err != nil {
		return nil, fmt.Errorf("error retrieving a reset token using the token hash:%w", err)
	}

	return &passwordResetToken, nil
}

// Create inserts an AuthRequestToken into the database
func (token *AuthResetToken) Create(db *sqlx.DB) error {
	if token.UserId == "" {
		return fmt.Errorf("a user id value must be specified to create a password reset token")
	}

	if token.TokenHash == "" {
		return fmt.Errorf("a token value must be specified to create a password reset token")
	}

	if token.ExpiresAt.IsZero() {
		return fmt.Errorf("a token must have an expiration date set create a password reset token")
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
		return fmt.Errorf("a user id value must be specified to delete a password reset token")
	}

	if token.TokenHash == "" {
		return fmt.Errorf("a token value must be specified to create a password reset token")
	}

	query := fmt.Sprintf(`
        DELETE FROM %s
        WHERE user_id = ? AND token_hash = ?
    `, userTokenTableName)

	_, err := db.Exec(query, token.UserId, token.TokenHash)
	return err
}

// DeleteAllTokensByUser deletes all of the tokens from the database for the specified user
func DeleteAllTokensByUser(db *sqlx.DB, userId string) error {
	if userId == "" {
		return fmt.Errorf("a user id value must be specified to delete the associated password reset tokens")
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
		return nil, NewErrUserNotSpecified(fmt.Errorf("unable to generate a new reset token token"))
	}

	if expiration <= 0 {
		return nil, fmt.Errorf("unable to create a new password reset token: the expiration value is invalid")
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
