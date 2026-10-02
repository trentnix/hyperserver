package config

import (
	"fmt"
	"strings"
	"time"
)

// Validate checks shared settings. Provider packages validate their own options.
func (c Config) Validate() error {
	if _, err := ParsePublicOrigin(c.HTTP.PublicOrigin); err != nil {
		return err
	}
	if c.Auth.ResetMinimumResponseTime < 0 {
		return fmt.Errorf("auth.resetMinimumResponseTime must not be negative")
	}
	if c.Auth.Enabled {
		if err := ValidateSigningKey("auth.jwtKey", c.Auth.JwtKey); err != nil {
			return err
		}
		if err := ValidateLifetime("auth.verificationTokenExpiration", c.Auth.VerificationTokenExpiration); err != nil {
			return err
		}
		if err := ValidateLifetime("auth.resetTokenExpiration", c.Auth.ResetTokenExpiration); err != nil {
			return err
		}
	}
	return nil
}

// ValidateSigningKey rejects missing and placeholder signing keys without logging them.
func ValidateSigningKey(field, value string) error {
	return validateSecret(field, value)
}

func validateSecret(field, value string) error {
	secret := strings.ToLower(strings.TrimSpace(value))
	if secret == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.HasPrefix(secret, "<") && strings.HasSuffix(secret, ">") {
		return fmt.Errorf("%s must not contain a placeholder", field)
	}
	for _, marker := range []string{"changeme", "change-me", "change_me", "replace-me", "replace_me", "your ", "your-", "your_", "placeholder"} {
		if strings.Contains(secret, marker) {
			return fmt.Errorf("%s must not contain a placeholder", field)
		}
	}
	return nil
}

// ValidateLifetime requires at least one second because tokens and cookies use seconds.
func ValidateLifetime(field string, value time.Duration) error {
	if value < time.Second {
		return fmt.Errorf("%s must be at least 1s", field)
	}
	return nil
}

// ProviderEnabled accepts the values understood by the current provider implementations.
func ProviderEnabled(field, value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "1":
		return true, nil
	case "false", "0", "":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true, false, 1, or 0", field)
	}
}
