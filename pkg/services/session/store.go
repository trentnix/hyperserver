// store.go provides a definition for the SessionStore interface and the errors that an
// implementation of the SessionStore interface might use
package session

import (
	"errors"
	"net/http"
	"strings"

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
