package sessioncookie

import (
	"time"

	"github.com/trentnix/hyperserver/config"
)

// ValidateOptions checks signing and expiration settings before a store is opened.
func ValidateOptions(signingKey string, tokenLifetime, cookieLifetime time.Duration) error {
	if err := config.ValidateSigningKey("session signing key", signingKey); err != nil {
		return err
	}
	if err := config.ValidateLifetime("session token lifetime", tokenLifetime); err != nil {
		return err
	}
	return config.ValidateLifetime("session cookie lifetime", cookieLifetime)
}
