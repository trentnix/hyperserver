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
	// A custom imported provider must work without adding its name to the loader.
	descriptors := []Descriptor{{Name: "custom", New: func() AuthService { return configTestAuthService{name: "custom"} }}}
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
		{"cookie authentication", func(c *config.Config) { c.HTTP.Session.Types["auth-user-session"] = "cookieStore" }, "session revocation"},
		{"cookie fallback", func(c *config.Config) { delete(c.HTTP.Session.Types, "auth-user-session") }, "session revocation"},
		{"SQLite fallback", func(c *config.Config) { c.HTTP.Session.Types = map[string]string{"default": "SQLITESTORE"} }, ""},
		{"disabled auth", func(c *config.Config) { *c = config.Config{} }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &config.Config{Auth: config.AuthConfig{Enabled: true, JwtKey: strings.Repeat("a", 32), VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour, Services: map[string]map[string]string{"custom": {"enabled": "true"}}}}
			c.HTTP.Session.Types = map[string]string{"default": "cookieStore", "auth-user-session": "sqliteStore"}
			tc.change(c)
			err := ValidateConfig(c, descriptors)
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
	for _, name := range []string{"custom", "CUSTOM"} {
		t.Run(name, func(t *testing.T) {
			descriptors := []Descriptor{
				{Name: "custom", New: func() AuthService { return configTestAuthService{name: "custom"} }},
				{Name: name, New: func() AuthService { return configTestAuthService{name: name} }},
			}
			c := &config.Config{Auth: config.AuthConfig{
				Enabled:                     true,
				JwtKey:                      strings.Repeat("a", 32),
				VerificationTokenExpiration: time.Hour,
				ResetTokenExpiration:        time.Hour,
				Services:                    map[string]map[string]string{"custom": {"enabled": "true"}},
			}}
			c.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
			if err := ValidateConfig(c, descriptors); err == nil || !strings.Contains(err.Error(), "duplicate registered auth service") {
				t.Fatalf("error = %v, want duplicate registration error", err)
			}
		})
	}
}
