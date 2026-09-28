package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func cleanConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "HYPERSERVER_") {
			t.Setenv(key, value)
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func loadTestConfig(t *testing.T, yaml string) (Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigFile(path)
	return readConfig(v)
}

func TestFileAndEnvironmentConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		env        map[string]string
		wantKey    string
		wantAge    time.Duration
		wantErr    string
	}{
		{name: "canonical file key", yaml: "http:\n  session:\n    jwtKey: file-key\n    tokenAge: 2h\n", wantKey: "file-key", wantAge: 2 * time.Hour},
		{name: "environment override", yaml: "http:\n  session:\n    jwtKey: file-key\n    tokenAge: 2h\n", env: map[string]string{"HYPERSERVER_HTTP_SESSION_JWTKEY": "environment-key", "HYPERSERVER_HTTP_SESSION_TOKENAGE": "3h"}, wantKey: "environment-key", wantAge: 3 * time.Hour},
		{name: "environment only fields", yaml: "app:\n  name: test\n", env: map[string]string{"HYPERSERVER_HTTP_SESSION_JWTKEY": "environment-key", "HYPERSERVER_HTTP_SESSION_TOKENAGE": "4h"}, wantKey: "environment-key", wantAge: 4 * time.Hour},
		{name: "empty override", yaml: "http:\n  session:\n    jwtKey: file-key\n", env: map[string]string{"HYPERSERVER_HTTP_SESSION_JWTKEY": ""}},
		{name: "malformed file lifetime", yaml: "http:\n  session:\n    tokenAge: tomorrow\n", wantErr: "tokenage"},
		{name: "malformed environment lifetime", yaml: "{}", env: map[string]string{"HYPERSERVER_AUTH_RESETTOKENEXPIRATION": "tomorrow"}, wantErr: "resettokenexpiration"},
		{name: "duration overflow", yaml: "http:\n  session:\n    tokenAge: 999999999999999999999h\n", wantErr: "tokenage"},
		{name: "invalid auth enabled", yaml: "auth:\n  enabled: perhaps\n", wantErr: "enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := loadTestConfig(t, tc.yaml)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErr)) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTP.Session.JwtKey != tc.wantKey || cfg.HTTP.Session.TokenAge != tc.wantAge {
				t.Fatal("loaded values do not match file/environment precedence")
			}
		})
	}
}

func TestEnvironmentProviderOptions(t *testing.T) {
	cleanConfigEnvironment(t)
	t.Setenv("HYPERSERVER_HTTP_SESSION_STORES_CUSTOM_ENABLED", "true")
	t.Setenv("HYPERSERVER_HTTP_SESSION_TYPES_DEFAULT", "custom")
	t.Setenv("HYPERSERVER_AUTH_SERVICES_CUSTOM_ENABLED", "true")
	c, err := loadTestConfig(t, "{}")
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTP.Session.Stores["custom"]["enabled"] != "true" || c.HTTP.Session.Types["default"] != "custom" || c.Auth.Services["custom"]["enabled"] != "true" {
		t.Fatal("environment-only provider selections were not loaded")
	}
}

func TestConfigLoadsDoNotShareState(t *testing.T) {
	cleanConfigEnvironment(t)
	if _, err := loadTestConfig(t, "app:\n  name: first\n"); err != nil {
		t.Fatal(err)
	}
	c, err := loadTestConfig(t, "{}")
	if err != nil {
		t.Fatal(err)
	}
	if c.App.Name != "" {
		t.Fatal("configuration leaked between loads")
	}
}

func TestSigningKeys(t *testing.T) {
	for _, key := range []string{"", "  ", "<your JWT encryption key goes here>", "change-me-to-a-long-secret-before-use", "placeholder-key-that-is-long-enough"} {
		err := ValidateSigningKey("test.jwtKey", key)
		if err == nil {
			t.Fatalf("accepted invalid key of length %d", len(key))
		}
		if key != "" && strings.Contains(err.Error(), key) {
			t.Fatal("error disclosed secret")
		}
	}
	if err := ValidateSigningKey("test.jwtKey", strings.Repeat("x", 32)); err != nil {
		t.Fatal(err)
	}
}

func TestValidateLifetimeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   time.Duration
		wantErr bool
	}{
		{"just below minimum", time.Second - time.Nanosecond, true},
		{"exact minimum", time.Second, false},
		{"just above minimum", time.Second + time.Nanosecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateLifetime("test.lifetime", tc.value)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateLifetime(%s) error = %v, wantErr = %v", tc.value, err, tc.wantErr)
			}
		})
	}
}

func TestProviderEnabled(t *testing.T) {
	for _, tc := range []struct {
		value         string
		want, wantErr bool
	}{
		{"true", true, false},
		{"TRUE", true, false},
		{"TrUe", true, false},
		{"1", true, false},
		{"false", false, false},
		{"FALSE", false, false},
		{"FaLsE", false, false},
		{"0", false, false},
		{"", false, false},
		{"yes", false, true},
		{"t", false, true},
		{"2", false, true},
		{" true ", false, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := ProviderEnabled("test.enabled", tc.value)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("ProviderEnabled(%q) = %v, %v, want %v, wantErr = %v", tc.value, got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestSharedConfigValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Config)
		want   string
	}{
		{"auth missing key", func(c *Config) { c.Auth.JwtKey = "" }, "auth.jwtKey"},
		{"auth placeholder", func(c *Config) { c.Auth.JwtKey = "<your auth token key goes here>" }, "auth.jwtKey"},
		{"missing verification age", func(c *Config) { c.Auth.VerificationTokenExpiration = 0 }, "auth.verificationTokenExpiration"},
		{"negative reset age", func(c *Config) { c.Auth.ResetTokenExpiration = -time.Second }, "auth.resetTokenExpiration"},
		{"subsecond reset age", func(c *Config) { c.Auth.ResetTokenExpiration = time.Millisecond }, "auth.resetTokenExpiration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{Auth: AuthConfig{Enabled: true, JwtKey: strings.Repeat("x", 32), VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour}}
			tc.change(&c)
			if err := c.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
	if err := (Config{}).Validate(); err != nil {
		t.Fatalf("disabled services must not require secrets: %v", err)
	}
}

func TestTemplateSessionSigningKey(t *testing.T) {
	cleanConfigEnvironment(t)
	template, err := os.ReadFile("config-template.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const fileKey = "test-session-signing-key"
	yaml := strings.Replace(string(template), "<your JWT encryption key goes here>", fileKey, 1)
	for _, tc := range []struct {
		name, environmentKey, want string
	}{
		{"file setting", "", fileKey},
		{"environment override", "environment-session-key", "environment-session-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HYPERSERVER_AUTH_ENABLED", "false")
			if tc.environmentKey != "" {
				t.Setenv("HYPERSERVER_HTTP_SESSION_JWTKEY", tc.environmentKey)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0600); err != nil {
				t.Fatal(err)
			}
			v := viper.New()
			v.SetConfigFile(filepath.Join(dir, "config.yaml"))
			cfg, err := readConfig(v)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTP.Session.JwtKey != tc.want {
				t.Fatal("session signing key did not load from the expected source")
			}
		})
	}
}
