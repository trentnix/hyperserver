package config

import (
	"strings"
	"testing"
)

func TestPublicOriginValidation(t *testing.T) {
	for _, value := range []string{"https://accounts.example", "http://localhost:8080/", "https://[::1]:8443", ""} {
		if err := (Config{HTTP: HTTPConfig{PublicOrigin: value}}).Validate(); err != nil {
			t.Errorf("valid origin %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"//attacker.example", "javascript:alert(1)", "https://", "https://user:secret@example.com", "https://example.com/path", "https://example.com?x=1", "https://example.com?", "https://example.com/#fragment", "https://example.com#", "https://example.com:bad", "https://example.com:", "https://example.com:0", "https://example.com:65536"} {
		// HTTP configuration must be validated even when auth is disabled.
		err := (Config{HTTP: HTTPConfig{PublicOrigin: value}}).Validate()
		if err == nil || !strings.Contains(err.Error(), "http.publicOrigin") {
			t.Errorf("invalid origin %q: error = %v", value, err)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Error("validation error disclosed configured credentials")
		}
	}
}
