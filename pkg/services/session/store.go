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

// checkStoreCookieConfig determines whether the configuration is configured sufficiently to
// manage sessions by using HTTP cookies and JWT tokens
func checkStoreCookieConfig(c *config.Config) error {
	cookieConfigurationInvalidErr := errors.New("cookie session store configuration is invalid")
	if c.HTTP.Session.JwtKey == "" {
		return NewErrTokenKeyNotSet(cookieConfigurationInvalidErr)
	}

	if c.HTTP.Session.CookieAge == 0 {
		return NewErrCookieLifetimeNotSet(cookieConfigurationInvalidErr)
	}

	if c.HTTP.Session.TokenAge == 0 {
		return NewErrTokenLifetimeNotSet(cookieConfigurationInvalidErr)
	}

	return nil
}
