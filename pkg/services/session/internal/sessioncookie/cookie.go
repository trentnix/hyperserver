package sessioncookie

import (
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/session"
)

// Validate checks syntax and size before reading or writing a session cookie.
func Validate(cookie *http.Cookie) error {
	if len(cookie.Name)+len(cookie.Value) > session.MaxCookieSize {
		return session.ErrSessionCookieTooLarge
	}
	if err := cookie.Valid(); err != nil {
		return err
	}
	if len(cookie.String()) > session.MaxCookieSize {
		return session.ErrSessionCookieTooLarge
	}
	return nil
}
