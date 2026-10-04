package session

import (
	"errors"
	"net/http"
)

const (
	// MaxCookieSize limits each session cookie, including its name and attributes.
	MaxCookieSize = 4096
	// MaxSessionDataSize limits session JSON before base64 encoding.
	MaxSessionDataSize    = 64 * 1024
	maxEncodedSessionSize = ((MaxSessionDataSize + 2) / 3) * 4
)

var (
	// ErrSessionTooLarge indicates that session data exceeds MaxSessionDataSize.
	ErrSessionTooLarge = errors.New("session data exceeds the size limit")
	// ErrSessionCookieTooLarge indicates that a session cookie exceeds MaxCookieSize.
	ErrSessionCookieTooLarge = errors.New("session cookie exceeds the size limit")
)

func validateSessionCookie(cookie *http.Cookie) error {
	if len(cookie.Name)+len(cookie.Value) > MaxCookieSize {
		return ErrSessionCookieTooLarge
	}
	if err := cookie.Valid(); err != nil {
		return err
	}
	if len(cookie.String()) > MaxCookieSize {
		return ErrSessionCookieTooLarge
	}
	return nil
}
