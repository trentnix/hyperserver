package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

// Validate checks shared settings. Provider packages validate their own options.
func (c Config) Validate() error {
	if timeout := c.App.InitializationTimeout; timeout != nil && *timeout <= 0 {
		return fmt.Errorf("app.initializationTimeout must be positive")
	}
	origin, err := ParsePublicOrigin(c.HTTP.PublicOrigin)
	if err != nil {
		return err
	}
	if _, err := requestinfo.NewProxyMiddleware(c.HTTP.TrustedProxies); err != nil {
		return fmt.Errorf("http.trustedProxies: %w", err)
	}
	if len(c.HTTP.TrustedProxies) > 0 && origin == nil {
		return fmt.Errorf("http.publicOrigin is required when http.trustedProxies is configured")
	}
	if c.HTTP.HSTSMaxAge < 0 {
		return fmt.Errorf("http.hstsMaxAge must not be negative")
	}
	if c.HTTP.TLS.Enabled {
		if strings.TrimSpace(c.HTTP.TLS.Certificate) == "" || strings.TrimSpace(c.HTTP.TLS.Key) == "" {
			return fmt.Errorf("http.tls.certificate and http.tls.key are required when http.tls.enabled is true")
		}
	} else if c.HTTP.TLS.Certificate != "" || c.HTTP.TLS.Key != "" {
		return fmt.Errorf("http.tls.enabled must be true when TLS certificate or key files are configured")
	}
	for _, item := range []struct {
		name  string
		limit RateLimitConfig
	}{
		{"http.defaultRateLimit", c.HTTP.DefaultRateLimit},
		{"http.sharedRateLimit", c.HTTP.SharedRateLimit},
	} {
		if !item.limit.Enabled {
			continue
		}
		if item.limit.Requests <= 0 {
			return fmt.Errorf("%s.requests must be positive", item.name)
		}
		if item.limit.Window <= 0 {
			return fmt.Errorf("%s.window must be positive", item.name)
		}
		if item.limit.MaxClients <= 0 {
			return fmt.Errorf("%s.maxClients must be positive", item.name)
		}
	}
	if c.Auth.ResetMinimumResponseTime < 0 {
		return fmt.Errorf("auth.resetMinimumResponseTime must not be negative")
	}
	if _, err := c.Auth.RateLimit.ClientCapacity(); err != nil {
		return fmt.Errorf("auth.rateLimit.%w", err)
	}
	if _, err := c.App.SiteRateLimit.ClientCapacity(); err != nil {
		return fmt.Errorf("app.siteRateLimit.%w", err)
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
