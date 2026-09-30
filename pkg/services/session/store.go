package session

import (
	"errors"
	"net/http"
	"strings"

	"github.com/trentnix/hyperserver/config"
)

// SessionStore loads, creates, saves, and ends named sessions.
// Callers must check each provider's revocation and persistence guarantees.
type (
	SessionStore interface {
		// Get reads a session, returning a new one when none exists.
		// Some providers return a new session alongside a decoding error.
		Get(r *http.Request, name string) (*Session, error)
		// New creates an unsaved session with a new ID.
		New(r *http.Request, name string) (*Session, error)
		// Save persists a session and writes its cookie before the response body.
		Save(w http.ResponseWriter, r *http.Request, session *Session) error
		// End ends a session according to the provider's revocation guarantees.
		End(w http.ResponseWriter, r *http.Request, session *Session) error
		// IsEnabled reports whether the store is enabled in configuration.
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
