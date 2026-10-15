package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
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
			err := ValidateSessionConfig(c)
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

func TestSessionSetupRejectsInvalidSelections(t *testing.T) {
	for _, provider := range []string{"cookieStore", "sqliteStore"} {
		for _, failure := range []string{"missing", "disabled", "malformed", "duplicate"} {
			t.Run(provider+"/"+failure, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "sessions.db")
				cfg := validSessionConfig()
				cfg.HTTP.Session.Types = map[string]string{"default": provider}
				cfg.HTTP.Session.Stores = map[string]map[string]string{
					provider: {"enabled": "true", "connection": path, "sessiontable": "sessions"},
				}
				switch failure {
				case "missing":
					delete(cfg.HTTP.Session.Stores, provider)
				case "disabled":
					cfg.HTTP.Session.Stores[provider]["enabled"] = "false"
				case "malformed":
					cfg.HTTP.Session.Stores[provider]["enabled"] = "perhaps"
				case "duplicate":
					cfg.HTTP.Session.Stores[strings.ToUpper(provider)] = map[string]string{"enabled": "true"}
				}

				manager, err := NewSessionManager(t.Context(), cfg)
				if manager != nil {
					t.Cleanup(func() { manager.Close() })
				}
				if manager != nil || err == nil {
					t.Fatalf("NewSessionManager = %v, %v, want nil and an error", manager, err)
				}
				if count, err := CleanupSessions(t.Context(), cfg, 10); count != 0 || err == nil {
					t.Fatalf("CleanupSessions = %d, %v, want zero and an error", count, err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("invalid selection created storage: %v", err)
				}
			})
		}
	}
}

func TestSessionSetupRejectsNilConfig(t *testing.T) {
	if err := ValidateSessionConfig(nil); err == nil {
		t.Fatal("validation accepted nil configuration")
	}
	if manager, err := NewSessionManager(t.Context(), nil); manager != nil || err == nil {
		t.Fatalf("NewSessionManager = %v, %v, want nil and an error", manager, err)
	}
	if count, err := CleanupSessions(t.Context(), nil, 10); count != 0 || err == nil {
		t.Fatalf("CleanupSessions = %d, %v, want zero and an error", count, err)
	}
}
