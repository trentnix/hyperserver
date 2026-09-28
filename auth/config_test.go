package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
)

type configTestAuthService struct {
	AuthService
	name string
}

func (s configTestAuthService) AuthType() string { return s.name }

func TestValidateAuthConfig(t *testing.T) {
	previous := authServices
	t.Cleanup(func() { authServices = previous })
	// A custom imported provider must work without adding its name to the loader.
	authServices = []AuthService{configTestAuthService{name: "custom"}}
	for _, tc := range []struct {
		name   string
		change func(*config.Config)
		want   string
	}{
		{"registered custom provider", func(c *config.Config) {}, ""},
		{"duplicate configured provider", func(c *config.Config) { c.Auth.Services["CUSTOM"] = map[string]string{"enabled": "true"} }, "duplicate configured auth service"},
		{"unknown enabled provider", func(c *config.Config) { c.Auth.Services["missing"] = map[string]string{"enabled": "true"} }, "unknown enabled auth service"},
		{"unknown disabled provider", func(c *config.Config) { c.Auth.Services["missing"] = map[string]string{"enabled": "false"} }, ""},
		{"invalid enable flag", func(c *config.Config) { c.Auth.Services["custom"]["enabled"] = "perhaps" }, "enabled"},
		{"no active provider", func(c *config.Config) { c.Auth.Services["custom"]["enabled"] = "false" }, "at least one enabled"},
		{"missing session default", func(c *config.Config) { c.HTTP.Session.Types = nil }, "types.default"},
		{"disabled auth", func(c *config.Config) { *c = config.Config{} }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &config.Config{Auth: config.AuthConfig{Enabled: true, JwtKey: strings.Repeat("a", 32), VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour, Services: map[string]map[string]string{"custom": {"enabled": "true"}}}}
			c.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
			tc.change(c)
			err := ValidateConfig(c)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateAuthConfigRejectsDuplicateRegistrations(t *testing.T) {
	previous := authServices
	t.Cleanup(func() { authServices = previous })
	for _, name := range []string{"custom", "CUSTOM"} {
		t.Run(name, func(t *testing.T) {
			authServices = []AuthService{
				configTestAuthService{name: "custom"},
				configTestAuthService{name: name},
			}
			c := &config.Config{Auth: config.AuthConfig{
				Enabled:                     true,
				JwtKey:                      strings.Repeat("a", 32),
				VerificationTokenExpiration: time.Hour,
				ResetTokenExpiration:        time.Hour,
				Services:                    map[string]map[string]string{"custom": {"enabled": "true"}},
			}}
			c.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
			if err := ValidateConfig(c); err == nil || !strings.Contains(err.Error(), "duplicate registered auth service") {
				t.Fatalf("error = %v, want duplicate registration error", err)
			}
		})
	}
}
