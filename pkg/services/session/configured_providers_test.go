package session_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	sessions "github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/cookie"
	"github.com/trentnix/hyperserver/pkg/services/session/sqlite"
)

func validSessionConfig() *config.Config {
	c := &config.Config{}
	c.HTTP.Session.JwtKey = strings.Repeat("s", 32)
	c.HTTP.Session.TokenAge = time.Hour
	c.HTTP.Session.CookieAge = time.Hour
	c.HTTP.Session.Stores = map[string]map[string]string{"cookiestore": {"enabled": "true"}}
	c.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	return c
}

func configuredTestProviders(cfg *config.Config) map[string]sessions.ConfiguredProvider {
	return map[string]sessions.ConfiguredProvider{
		"cookiestore": cookie.ConfiguredProvider(*cfg),
		"sqlitestore": sqlite.ConfiguredProvider(*cfg),
	}
}

func TestValidateSessionConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*config.Config)
		want   string
	}{
		{"valid", func(c *config.Config) {}, ""},
		{"missing key", func(c *config.Config) { c.HTTP.Session.JwtKey = "" }, "http.session.jwtKey"},
		{"short key", func(c *config.Config) { c.HTTP.Session.JwtKey = strings.Repeat("s", 31) }, "http.session.jwtKey must be at least 32 bytes"},
		{"placeholder key", func(c *config.Config) { c.HTTP.Session.JwtKey = "<your JWT encryption key goes here>" }, "http.session.jwtKey"},
		{"missing token age", func(c *config.Config) { c.HTTP.Session.TokenAge = 0 }, "http.session.tokenAge"},
		{"negative token age", func(c *config.Config) { c.HTTP.Session.TokenAge = -time.Hour }, "http.session.tokenAge"},
		{"negative cookie age", func(c *config.Config) { c.HTTP.Session.CookieAge = -time.Hour }, "http.session.cookieAge"},
		{"subsecond cookie age", func(c *config.Config) { c.HTTP.Session.CookieAge = time.Millisecond }, "http.session.cookieAge"},
		{"unknown enabled store", func(c *config.Config) { c.HTTP.Session.Stores["redis"] = map[string]string{"enabled": "true"} }, "unknown enabled session store"},
		{"unknown disabled store", func(c *config.Config) { c.HTTP.Session.Stores["redis"] = map[string]string{"enabled": "false"} }, ""},
		{"invalid enable flag", func(c *config.Config) { c.HTTP.Session.Stores["cookiestore"]["enabled"] = "perhaps" }, "enabled"},
		{"disabled selected store", func(c *config.Config) { c.HTTP.Session.Stores["cookiestore"]["enabled"] = "false" }, "unknown or disabled store"},
		{"unknown selection", func(c *config.Config) { c.HTTP.Session.Types["user"] = "redis" }, "http.session.types.user"},
		{"missing default", func(c *config.Config) { delete(c.HTTP.Session.Types, "default") }, "types.default"},
		{"duplicate store", func(c *config.Config) { c.HTTP.Session.Stores["CookieStore"] = map[string]string{"enabled": "true"} }, "duplicate session store"},
		{"incomplete SQLite options", func(c *config.Config) { c.HTTP.Session.Stores["sqlitestore"] = map[string]string{"enabled": "true"} }, "connection and sessionTable"},
		{"SQLite options checked without I/O", func(c *config.Config) {
			c.HTTP.Session.Stores["sqlitestore"] = map[string]string{"enabled": "1", "connection": "/nonexistent-directory/test.db", "sessiontable": "session"}
			c.HTTP.Session.Types["default"] = "sqliteStore"
		}, ""},
		{"disabled sessions", func(c *config.Config) { *c = config.Config{} }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := validSessionConfig()
			tc.change(c)
			err := sessions.ValidateConfiguredStores(c, configuredTestProviders(c))
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

func TestManagerRejectsMissingSelection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types map[string]string
	}{
		{"unknown named store", map[string]string{"user": "missing", "default": "cookieStore"}},
		{"unknown default store", map[string]string{"default": "missing"}},
		{"empty named store", map[string]string{"user": "", "default": "cookieStore"}},
		{"empty default store", map[string]string{"default": ""}},
		{"missing default", map[string]string{"other": "cookieStore"}},
		{"no selections", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSessionConfig()
			cfg.HTTP.Session.Types = tc.types
			manager, err := sessions.NewConfiguredManager(context.Background(), cfg, configuredTestProviders(cfg))
			if manager != nil || err == nil {
				t.Fatalf("NewSessionManager = %v, %v, want nil and an error", manager, err)
			}
		})
	}
}

func TestGetStoreSelectsCookieStore(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types map[string]string
	}{
		{"named store", map[string]string{"user": "cookieStore", "default": "cookieStore"}},
		{"default fallback", map[string]string{"default": "cookieStore"}},
		{"case insensitive provider", map[string]string{"user": "COOKIESTORE", "default": "cookieStore"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSessionConfig()
			cfg.HTTP.Session.Types = tc.types
			manager, err := sessions.NewConfiguredManager(context.Background(), cfg, configuredTestProviders(cfg))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { manager.Close() })
			store, err := manager.Store("user")
			if err != nil {
				t.Fatal(err)
			}
			if cookie, ok := store.(*cookie.CookieStore); !ok || !cookie.IsEnabled() {
				t.Fatalf("getStore = %T, want enabled CookieStore", store)
			}
		})
	}
}

func TestManagerRejectsDisabledStore(t *testing.T) {
	cfg := validSessionConfig()
	cfg.HTTP.Session.Stores["cookiestore"]["enabled"] = "false"
	manager, err := sessions.NewConfiguredManager(context.Background(), cfg, configuredTestProviders(cfg))
	if manager != nil || err == nil {
		t.Fatalf("NewSessionManager = %v, %v, want nil and an error", manager, err)
	}
}
