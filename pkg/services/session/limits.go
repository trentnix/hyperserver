package session

import "errors"

const (
	// MaxCookieSize limits each session cookie, including its name and attributes.
	MaxCookieSize = 4096
	// MaxSessionDataSize limits serialized session bytes before base64 encoding.
	MaxSessionDataSize = 64 * 1024
	// MaxEncodedSessionSize bounds the base64 representation read by providers.
	MaxEncodedSessionSize = ((MaxSessionDataSize + 2) / 3) * 4
)

var (
	// ErrSessionTooLarge indicates that session data exceeds MaxSessionDataSize.
	ErrSessionTooLarge = errors.New("session data exceeds the size limit")
	// ErrSessionCookieTooLarge indicates that a session cookie exceeds MaxCookieSize.
	ErrSessionCookieTooLarge = errors.New("session cookie exceeds the size limit")
)
