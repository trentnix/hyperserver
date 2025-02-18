// store.go provides a definition for the SessionStore interface and the errors that an
// implementation of the SessionStore interface might use
package session

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dgrijalva/jwt-go"
	"github.com/trentnix/hyperserver/config"
)

type (
	SessionStore interface {
		Get(r *http.Request, name string) (*Session, error)
		New(r *http.Request, name string) (*Session, error)
		Save(r *http.Request, w http.ResponseWriter, session *Session) error
		End(r *http.Request, w http.ResponseWriter, session *Session) error
		IsEnabled() bool
	}

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

var (
	ErrSessionKeyInvalid    = errors.New("the session key is invalid")
	ErrInvalidToken         = errors.New("invalid token")
	ErrTokenKeyNotSet       = errors.New("the JWT encryption key was not set")
	ErrCookieLifetimeNotSet = errors.New("the lifetime of an HTTP cookie used to store session information is not set")
	ErrTokenLifetimeNotSet  = errors.New("the lifetime of a JWT token used to store session information is not set")
)

// getStoreConfigOptions retrieves the configuration options map from the Session.Stores
// configuration settings for the specified session store type
func getStoreConfigOptions(c *config.Config, storeType string) map[string]string {
	for configOption, configData := range c.HTTP.Session.Stores {
		if strings.EqualFold(configOption, storeType) {
			return configData
		}
	}

	return nil
}

// checkStoreConfig determines whether the configuration is configured sufficiently to
// manage sessions by using HTTP cookies and JWT tokens
func checkStoreCookieConfig(c *config.Config) error {
	if c.HTTP.Session.JwtKey == "" {
		return ErrTokenKeyNotSet
	}

	if c.HTTP.Session.CookieAge == 0 {
		return ErrCookieLifetimeNotSet
	}

	if c.HTTP.Session.TokenAge == 0 {
		return ErrTokenLifetimeNotSet
	}

	return nil
}

// parseJWT parses the specified token into a SessionClaims instance to extract
// session data
func parseSessionJWT(tokenString string, jwtKey []byte) (*SessionClaims, error) {
	if tokenString == "" {
		return nil, ErrInvalidToken
	}

	// Parse and validate the JWT
	token, err := jwt.ParseWithClaims(tokenString, &SessionClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Ensure the token is signed with the expected method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
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

	return nil, ErrInvalidToken
}
