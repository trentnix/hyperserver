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

// AuthToken holds an account token and its storage metadata.
// Create persists the hash, account ID, purpose, and expiry, but not the raw Token.
type (
	// AuthToken defines the data that will be serialized to the database to
	// manage a reset token
	AuthToken struct {
		UserId string `db:"user_id"`
		// TokenHash is the SHA-256 hash stored instead of the raw token.
		TokenHash string `db:"token_hash"`
		Token     string
		ExpiresAt time.Time `db:"expires_at"`
		Type      string    `db:"token_type"`
	}
)

// Create inserts the token's hash, account ID, expiry, and purpose.
// The account schema must already exist. It does not start a transaction with an account change.
func (token *AuthToken) Create(db *sqlx.DB) error {
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
	if token.Type != resetTokenType && token.Type != verificationTokenType {
		return NewErrToken(errors.New("invalid token purpose"))
	}

	query := fmt.Sprintf(`
        INSERT INTO %s (user_id, token_hash, expires_at, token_type)
        VALUES (:user_id, :token_hash, :expires_at, :token_type)
    `, userTokenTableName)

	_, err := db.NamedExec(query, token)
	return err
}

// Delete removes the record matching UserId and TokenHash.
// It returns ErrTokenNotFound if no row was deleted.
func (token *AuthToken) Delete(db *sqlx.DB) error {
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

// getTokenByUser retrieves an AuthToken for the specified user from the database. The
// token's type must be provided to differentiate between other auth tokens that may be stored.
func getTokenByUser(db *sqlx.DB, userId string, tokenType string) (*AuthToken, error) {
	if userId == "" {
		return nil, NewErrUserNotSpecified(fmt.Errorf("get token by user"))
	}

	var token AuthToken

	query := fmt.Sprintf(`
        SELECT user_id, token_hash, expires_at, token_type
        FROM %s
        WHERE user_id = ? AND token_type = ?
    `, userTokenTableName)

	err := db.Get(&token, query, userId, tokenType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No row was found, handle accordingly.
			return nil, NewErrTokenNotFound(fmt.Errorf("get token by user: %s", userId))
		}
		return nil, database.NewErrDatabase(fmt.Errorf("get token by user: %s: %w", userId, err))
	}

	return &token, nil
}

// getTokenByHash retrieves an AuthToken for the specified token value from the database. The
// token's type must be provided to make sure the caller is aware of the token's context.
func getTokenByHash(db *sqlx.DB, token string, tokenType string) (*AuthToken, error) {
	if token == "" {
		return nil, NewErrTokenNotSpecified(fmt.Errorf("get token by value"))
	}

	var passwordResetToken AuthToken

	query := fmt.Sprintf(`
        SELECT user_id, token_hash, expires_at, token_type
        FROM %s
        WHERE token_hash = ? AND token_type = ?
    `, userTokenTableName)

	err := db.Get(&passwordResetToken, query, token, tokenType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// no row was found, handle accordingly.
			return nil, NewErrTokenNotFound(fmt.Errorf("get token by value"))
		}
		return nil, database.NewErrDatabase(fmt.Errorf("get token by value: %w", err))
	}

	return &passwordResetToken, nil
}

func newAuthToken(userId string, jwtKey []byte, expiration time.Duration, tokenType string) (*AuthToken, error) {
	errNewToken := errors.New("error creating a new token instance")
	if userId == "" {
		return nil, NewErrUserNotSpecified(errNewToken)
	}

	if expiration <= 0 {
		return nil, NewErrTokenExpirationNotSpecified(errNewToken)
	}
	if len(jwtKey) == 0 {
		return nil, NewErrJwtKeyNotSet(errNewToken)
	}
	if tokenType != resetTokenType && tokenType != verificationTokenType {
		return nil, NewErrToken(errors.New("invalid token purpose"))
	}

	// create token - user.id and expiration
	expirationTime := time.Now().Add(expiration)

	claims := &VerificationClaims{
		Id:      userId,
		Purpose: tokenType,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
		},
	}

	// Create and sign the token with the specified algorithm and claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	verificationToken, err := token.SignedString(jwtKey)
	if err != nil {
		return nil, NewErrToken(err)
	}

	hash := sha256.Sum256([]byte(verificationToken))
	verificationTokenHash := hex.EncodeToString(hash[:])

	authToken := &AuthToken{}
	authToken.UserId = userId
	authToken.TokenHash = verificationTokenHash
	authToken.Token = verificationToken
	authToken.ExpiresAt = expirationTime
	authToken.Type = tokenType

	// return token
	return authToken, nil
}

// parseAuthToken checks the signature and purpose before a token can authorize an action.
func parseAuthToken(tokenString string, jwtKey []byte, purpose string) (*VerificationClaims, error) {
	if len(jwtKey) == 0 {
		return nil, NewErrJwtKeyNotSet(errors.New("a signing key is required to validate a token"))
	}

	claims := &VerificationClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected token signing method")
		}
		return jwtKey, nil
	})
	if err != nil {
		var validationErr *jwt.ValidationError
		if errors.As(err, &validationErr) && validationErr.Errors == jwt.ValidationErrorExpired {
			return nil, NewErrTokenExpired(err)
		}
		return nil, NewErrToken(errors.New("invalid signed token"))
	}

	if !token.Valid || claims.Id == "" || claims.Purpose != purpose || claims.ExpiresAt == 0 {
		return nil, NewErrToken(errors.New("invalid token claims"))
	}
	return claims, nil
}

func validateToken(db *sqlx.DB, tokenString string, jwtKey []byte, tokenType string) (*User, error) {
	errValidateReset := errors.New("error validating the specified reset token")
	if tokenString == "" {
		return nil, NewErrTokenNotSpecified(errValidateReset)
	}
	claims, err := parseAuthToken(tokenString, jwtKey, tokenType)
	if err != nil {
		return nil, err
	}

	// hash the received token
	hash := sha256.Sum256([]byte(tokenString))
	tokenHash := hex.EncodeToString(hash[:])

	// retrieve the token record from the database
	passwordResetToken, err := getTokenByHash(db, tokenHash, tokenType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, NewErrTokenNotFound(err)
		}

		return nil, err
	}
	if claims.Id != passwordResetToken.UserId {
		return nil, NewErrToken(errors.New("token does not belong to the stored account"))
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
