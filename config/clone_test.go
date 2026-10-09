package config

import (
	"reflect"
	"testing"
	"time"
)

func TestConfigClone(t *testing.T) {
	timeout := time.Minute
	original := Config{
		HTTP: HTTPConfig{PublicOrigin: "https://example.com", TrustedProxies: []string{"127.0.0.1/32"}},
		Auth: AuthConfig{
			Services:       map[string]map[string]string{"email": {"enabled": "true"}, "unset": nil},
			AccountStorage: AccountStorageConfig{Provider: "custom", Options: map[string]string{"bucket": "users"}},
			RateLimit:      &ModuleRateLimitConfig{MaxClients: 100},
		},
		App: AppConfig{Name: "original", SiteRateLimit: &ModuleRateLimitConfig{MaxClients: 200}, InitializationTimeout: &timeout},
	}
	original.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	original.HTTP.Session.Stores = map[string]map[string]string{"cookieStore": {"enabled": "true"}, "unset": nil}

	for name, mutate := range map[string]func(*Config){
		"initialization timeout":   func(c *Config) { *c.App.InitializationTimeout = time.Second },
		"scalar":                   func(c *Config) { c.App.Name = "changed" },
		"proxy slice":              func(c *Config) { c.HTTP.TrustedProxies[0] = "192.0.2.0/24" },
		"session mapping":          func(c *Config) { c.HTTP.Session.Types["default"] = "sqliteStore" },
		"session provider options": func(c *Config) { c.HTTP.Session.Stores["cookieStore"]["enabled"] = "false" },
		"session provider map":     func(c *Config) { delete(c.HTTP.Session.Stores, "cookieStore") },
		"auth provider options":    func(c *Config) { c.Auth.Services["email"]["enabled"] = "false" },
		"auth provider map":        func(c *Config) { delete(c.Auth.Services, "email") },
		"account options":          func(c *Config) { c.Auth.AccountStorage.Options["bucket"] = "other" },
		"auth rate limit":          func(c *Config) { c.Auth.RateLimit.MaxClients++ },
		"site rate limit":          func(c *Config) { c.App.SiteRateLimit.MaxClients++ },
	} {
		t.Run(name, func(t *testing.T) {
			copy := original.Clone()
			if !reflect.DeepEqual(copy, original) {
				t.Fatal("clone changed configuration values")
			}
			before := original.Clone()
			mutate(&copy)
			if reflect.DeepEqual(copy, original) {
				t.Fatal("test mutation did not change the clone")
			}
			if !reflect.DeepEqual(original, before) {
				t.Fatal("clone shares mutable settings with its source")
			}
		})
	}
	if got := (Config{}).Clone(); !reflect.DeepEqual(got, Config{}) {
		t.Fatal("clone changed nil defaults")
	}
	empty := Config{HTTP: HTTPConfig{TrustedProxies: []string{}}}
	empty.HTTP.Session.Stores = map[string]map[string]string{}
	if !reflect.DeepEqual(empty, empty.Clone()) {
		t.Fatal("clone changed empty collections")
	}
}
