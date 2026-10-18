package sessioncookie

import (
	"time"

	"github.com/trentnix/hyperserver/config"
)

// ValidateConfig checks the shared settings used by the built-in session providers.
func ValidateConfig(signingKey string, tokenLifetime, cookieLifetime time.Duration) error {
	if err := config.ValidateSigningKey("http.session.jwtKey", signingKey); err != nil {
		return err
	}
	if err := config.ValidateLifetime("http.session.tokenAge", tokenLifetime); err != nil {
		return err
	}
	return config.ValidateLifetime("http.session.cookieAge", cookieLifetime)
}

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
