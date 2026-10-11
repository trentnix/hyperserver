package stores

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	sessions "github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	// SessionClaims contains the session data that is serialized to and from a JWT
	SessionClaims struct {
		// identifies a unique session
		ID string `json:"id"`
		// Purpose distinguishes session cookies from account-action tokens.
		Purpose string `json:"purpose"`
		// values that can be stored in a session
		Value string `json:"data"`
		// RegisteredClaims carries signed lifetime metadata.
		jwt.RegisteredClaims
	}
)

const sessionTokenPurpose = "session"

// ExpiresAtTime returns the signed expiration, or zero time if it is absent.
func (c *SessionClaims) ExpiresAtTime() time.Time {
	if c.ExpiresAt == nil {
		return time.Time{}
	}
	return c.ExpiresAt.Time
}

// parseSessionJWT validates a signed session token and extracts its claims.
func parseSessionJWT(tokenString string, jwtKey []byte) (*SessionClaims, error) {
	if len(tokenString) > sessions.MaxCookieSize {
		return nil, sessions.NewErrInvalidToken(sessions.ErrSessionCookieTooLarge)
	}
	if tokenString == "" {
		return nil, sessions.NewErrInvalidToken(fmt.Errorf("the token string is empty"))
	}

	if len(jwtKey) == 0 {
		return nil, sessions.NewErrTokenKeyNotSet(fmt.Errorf("unable to parse the JWT"))
	}

	claims := &SessionClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
		return jwtKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil {
		return nil, sessions.NewErrInvalidToken(err)
	}

	if !token.Valid || claims.ID == "" || claims.Purpose != sessionTokenPurpose {
		return nil, sessions.NewErrInvalidToken(fmt.Errorf("invalid session claims"))
	}
	return claims, nil
}
