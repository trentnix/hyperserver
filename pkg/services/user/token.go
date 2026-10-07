package user

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
)

type (
	// AuthToken holds an account token and its storage metadata.
	// Create persists the hash, account ID, purpose, and expiry, but not the raw Token.
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
	return token.CreateContext(context.Background(), db)
}

// CreateContext stores token metadata using ctx. It never stores the raw token.
func (token *AuthToken) CreateContext(ctx context.Context, db *sqlx.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}
	errCreateToken := errors.New("error creating a new token in the database")
	if token == nil || token.UserId == "" {
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

	_, err := db.NamedExecContext(ctx, query, token)
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

func newAuthToken(u *User, jwtKey []byte, expiration time.Duration, tokenType string) (*AuthToken, error) {
	errNewToken := errors.New("error creating a new token instance")
	if u == nil || u.ID == "" {
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
	if tokenType == verificationTokenType && u.Email == "" {
		return nil, NewErrToken(errors.New("verification requires an email address"))
	}

	tokenID, err := uuid.NewRandom()
	if err != nil {
		return nil, NewErrToken(err)
	}
	expirationTime := time.Now().Add(expiration)

	// A random token ID prevents same-second resends from reissuing a consumed token.
	claims := &VerificationClaims{
		Id:      u.ID,
		Purpose: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        tokenID.String(),
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}
	if tokenType == verificationTokenType {
		claims.Email = u.Email
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
	authToken.UserId = u.ID
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
	token, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
		return jwtKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, NewErrTokenExpired(err)
		}
		return nil, NewErrToken(errors.New("invalid signed token"))
	}

	if !token.Valid || claims.Id == "" || claims.Purpose != purpose {
		return nil, NewErrToken(errors.New("invalid token claims"))
	}
	if purpose == verificationTokenType && claims.Email == "" {
		return nil, NewErrToken(errors.New("verification token has no email address"))
	}
	return claims, nil
}

// consumeStoredToken matches a token's storage identity and checks both expiry
// deadlines after acquiring the write lock. The caller must roll back on failure.
func consumeStoredToken(ctx context.Context, tx *sqlx.Tx, accountID, tokenHash, purpose string, signedExpiry time.Time) error {
	var expiresAt time.Time
	err := tx.QueryRowContext(ctx, `DELETE FROM `+userTokenTableName+`
		WHERE user_id = ? AND token_hash = ? AND token_type = ?
		RETURNING expires_at`, accountID, tokenHash, purpose).
		Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return NewErrTokenNotFound(err)
	}
	if err != nil {
		return err
	}

	now := time.Now()
	if !now.Before(expiresAt) || !now.Before(signedExpiry) {
		return NewErrTokenExpired(nil)
	}
	return nil
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

	if tokenType == verificationTokenType && user.Email != claims.Email {
		return nil, NewErrToken(errors.New("verification address no longer matches the account"))
	}
	return user, nil
}
