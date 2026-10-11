package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type (
	// AuthToken holds an account token and its storage metadata.
	// Pass Metadata to AccountRepository.InsertToken to persist it without the raw Token.
	AuthToken struct {
		UserId string
		// TokenHash is the SHA-256 hash stored instead of the raw token.
		TokenHash string
		Token     string
		ExpiresAt time.Time
		Type      string
	}
)

// Metadata returns the storage fields without the raw signed token.
func (token AuthToken) Metadata() TokenMetadata {
	return TokenMetadata{
		AccountID: token.UserId, TokenHash: token.TokenHash,
		Purpose: token.Type, ExpiresAt: token.ExpiresAt,
	}
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

func validateToken(ctx context.Context, accounts AccountRepository, tokenString string, jwtKey []byte, tokenType string) (*User, error) {
	errValidateReset := errors.New("error validating the specified reset token")
	if tokenString == "" {
		return nil, NewErrTokenNotSpecified(errValidateReset)
	}
	if accounts == nil {
		return nil, errors.New("the account repository is not configured")
	}
	claims, err := parseAuthToken(tokenString, jwtKey, tokenType)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256([]byte(tokenString))
	tokenHash := hex.EncodeToString(hash[:])
	stored, err := accounts.GetToken(ctx, tokenHash, tokenType)
	if err != nil {
		return nil, err
	}
	if stored.AccountID != claims.Id || stored.TokenHash != tokenHash || stored.Purpose != tokenType {
		return nil, NewErrToken(errors.New("token does not belong to the stored account"))
	}
	if !time.Now().Before(stored.ExpiresAt) {
		return nil, NewErrTokenExpired(errValidateReset)
	}

	account, err := accounts.GetByID(ctx, stored.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.ID != claims.Id {
		return nil, NewErrUserNotFound(nil)
	}
	if tokenType == verificationTokenType && account.Email != claims.Email {
		return nil, NewErrToken(errors.New("verification address no longer matches the account"))
	}

	// Either deadline may have passed while waiting for storage.
	now := time.Now()
	if !now.Before(claims.ExpiresAt.Time) || !now.Before(stored.ExpiresAt) {
		return nil, NewErrTokenExpired(errValidateReset)
	}
	return account, nil
}
