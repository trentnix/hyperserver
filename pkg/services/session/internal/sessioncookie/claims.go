// Package sessioncookie implements the signed-cookie format shared by session providers.
package sessioncookie

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
	sessions "github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	// Claims contains the session data that is serialized to and from a JWT
	Claims struct {
		// identifies a unique session
		ID string `json:"id"`
		// Name binds the token to its named session.
		Name string `json:"name"`
		// Purpose distinguishes session cookies from account-action tokens.
		Purpose string `json:"purpose"`
		// values that can be stored in a session
		Value string `json:"data"`
		// RegisteredClaims carries signed lifetime metadata.
		jwt.RegisteredClaims
	}
)

// Purpose distinguishes session cookies from other signed tokens.
const Purpose = "session"

// ExpiresAtTime returns the signed expiration, or zero time if it is absent.
func (c *Claims) ExpiresAtTime() time.Time {
	if c.ExpiresAt == nil {
		return time.Time{}
	}
	return c.ExpiresAt.Time
}

// Parse validates a signed session token and extracts its claims.
func Parse(tokenString string, jwtKey []byte, name string) (*Claims, error) {
	if len(tokenString) > sessions.MaxCookieSize {
		return nil, hs_errors.WithCause(sessions.ErrInvalidToken, sessions.ErrSessionCookieTooLarge)
	}
	if tokenString == "" {
		return nil, hs_errors.WithCause(sessions.ErrInvalidToken, fmt.Errorf("the token string is empty"))
	}

	if len(jwtKey) == 0 {
		return nil, hs_errors.WithCause(sessions.ErrTokenKeyNotSet, fmt.Errorf("unable to parse the JWT"))
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
		return jwtKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil {
		return nil, hs_errors.WithCause(sessions.ErrInvalidToken, err)
	}

	if !token.Valid || claims.ID == "" || claims.Purpose != Purpose || name == "" || claims.Name != name {
		return nil, hs_errors.WithCause(sessions.ErrInvalidToken, fmt.Errorf("invalid session claims"))
	}
	return claims, nil
}
