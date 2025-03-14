// claims.go defines the SessionClaims struct that is serialized to a JWT
package session

import (
	"fmt"
	"time"

	"github.com/dgrijalva/jwt-go"
)

type (
	// SessionClaims contains the session data that is serialized to and from a JWT
	SessionClaims struct {
		// identifies a unique session
		ID string `json:"id"`
		// values that can be stored in a session
		Data map[string]string `json:"data"`
		// stanard JWT claims embedded
		jwt.StandardClaims
	}
)

// ExpiresAtTime returns the expiration time as time.Time
func (c *SessionClaims) ExpiresAtTime() time.Time {
	return time.Unix(c.ExpiresAt, 0)
}

// parseJWT parses the specified token into a SessionClaims instance to extract
// session data
func parseSessionJWT(tokenString string, jwtKey []byte) (*SessionClaims, error) {
	if tokenString == "" {
		return nil, NewErrInvalidToken(fmt.Errorf("the token string is empty"))
	}

	if len(jwtKey) == 0 {
		return nil, NewErrTokenKeyNotSet(fmt.Errorf("unable to parse the JWT"))
	}

	// Parse and validate the JWT
	token, err := jwt.ParseWithClaims(tokenString, &SessionClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, NewErrInvalidToken(fmt.Errorf("the token is not signed with the expected method"))
		}
		return jwtKey, nil
	})
	if err != nil {
		return nil, err
	}

	// Extract claims
	if claims, ok := token.Claims.(*SessionClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, NewErrInvalidToken(fmt.Errorf("unable to parse the JWT"))
}
