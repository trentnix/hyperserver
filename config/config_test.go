package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	return readConfig(file, os.Environ())
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

func TestAccountStorageConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, provider, connection string
		env                              map[string]string
	}{
		{name: "omitted", yaml: "{}"},
		{name: "file", yaml: "auth:\n  accountStorage:\n    provider: custom\n    options:\n      connection: file-value\n", provider: "custom", connection: "file-value"},
		{name: "environment only", yaml: "{}", env: map[string]string{"HYPERSERVER_AUTH_ACCOUNTSTORAGE_PROVIDER": "custom", "HYPERSERVER_AUTH_ACCOUNTSTORAGE_OPTIONS_CONNECTION": "env-value"}, provider: "custom", connection: "env-value"},
		{name: "environment override", yaml: "auth:\n  accountStorage:\n    provider: sqlite\n    options:\n      connection: file-value\n", env: map[string]string{"HYPERSERVER_AUTH_ACCOUNTSTORAGE_PROVIDER": "custom", "HYPERSERVER_AUTH_ACCOUNTSTORAGE_OPTIONS_CONNECTION": "env-value"}, provider: "custom", connection: "env-value"},
		{name: "empty option override", yaml: "auth:\n  accountStorage:\n    provider: custom\n    options:\n      connection: file-value\n", env: map[string]string{"HYPERSERVER_AUTH_ACCOUNTSTORAGE_OPTIONS_CONNECTION": ""}, provider: "custom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := loadTestConfig(t, tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Auth.AccountStorage; got.Provider != tc.provider || got.Options["connection"] != tc.connection {
				t.Fatalf("account storage configuration = %+v", got)
			}
		})
	}
}

func TestPublicOriginConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, override, want string
	}{
		{name: "file", yaml: "http:\n  publicOrigin: https://file.example\n", want: "https://file.example"},
		{name: "environment only", yaml: "{}", override: "https://env.example", want: "https://env.example"},
		{name: "environment override", yaml: "http:\n  publicOrigin: https://file.example\n", override: "https://env.example", want: "https://env.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			if tc.override != "" {
				t.Setenv("HYPERSERVER_HTTP_PUBLICORIGIN", tc.override)
			}
			cfg, err := loadTestConfig(t, tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.HTTP.PublicOrigin; got != tc.want {
				t.Fatalf("publicOrigin = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHTTPListenConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		env        map[string]string
		wantHost   string
		wantPort   uint16
		wantOrigin string
	}{
		{name: "default", yaml: "{}", wantHost: "127.0.0.1"},
		{name: "file", yaml: "http:\n  listenHost: localhost\n  port: 8081\n", wantHost: "localhost", wantPort: 8081},
		{name: "environment only", yaml: "{}", env: map[string]string{"HYPERSERVER_HTTP_LISTENHOST": "::1", "HYPERSERVER_HTTP_PORT": "8082"}, wantHost: "::1", wantPort: 8082},
		{name: "environment override", yaml: "http:\n  listenHost: localhost\n  port: 8081\n", env: map[string]string{"HYPERSERVER_HTTP_LISTENHOST": "127.0.0.2", "HYPERSERVER_HTTP_PORT": "8082"}, wantHost: "127.0.0.2", wantPort: 8082},
		{name: "empty override", yaml: "http:\n  listenHost: localhost\n", env: map[string]string{"HYPERSERVER_HTTP_LISTENHOST": ""}},
		{name: "independent public origin", yaml: "http:\n  listenHost: 127.0.0.1\n  port: 8080\n  publicOrigin: https://example.com\n", wantHost: "127.0.0.1", wantPort: 8080, wantOrigin: "https://example.com"},
		{name: "mail hostname unchanged", yaml: "mail:\n  hostname: smtp.example.invalid\n", wantHost: "127.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := loadTestConfig(t, tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTP.ListenHost != tc.wantHost || cfg.HTTP.Port != tc.wantPort || cfg.HTTP.PublicOrigin != tc.wantOrigin {
				t.Fatalf("listenHost=%q, port=%d, publicOrigin=%q", cfg.HTTP.ListenHost, cfg.HTTP.Port, cfg.HTTP.PublicOrigin)
			}
			if tc.name == "mail hostname unchanged" && cfg.Mail.Hostname != "smtp.example.invalid" {
				t.Fatal("HTTP setting rename changed the SMTP host")
			}
		})
	}
}

func TestLegacyHTTPHostnameRejected(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		env        map[string]string
	}{
		{name: "file", yaml: "http:\n  hostname: 127.0.0.1\n"},
		{name: "empty file value", yaml: "http:\n  hostname: ''\n"},
		{name: "both names", yaml: "http:\n  hostname: localhost\n  listenHost: 127.0.0.1\n"},
		{name: "environment", yaml: "{}", env: map[string]string{"HYPERSERVER_HTTP_HOSTNAME": "localhost"}},
		{name: "empty environment", yaml: "{}", env: map[string]string{"HYPERSERVER_HTTP_HOSTNAME": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			_, err := loadTestConfig(t, tc.yaml)
			if err == nil || !strings.Contains(err.Error(), "http.listenHost") || !strings.Contains(err.Error(), "HYPERSERVER_HTTP_LISTENHOST") {
				t.Fatalf("error = %v, want instructions for renaming the listener setting", err)
			}
		})
	}
}

func TestRegistrationConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, override string
		want, wantError      bool
	}{
		{name: "omitted disables registration", yaml: "{}"},
		{name: "enabled in file", yaml: "auth:\n  registrationEnabled: true", want: true},
		{name: "enabled by environment", yaml: "{}", override: "true", want: true},
		{name: "disabled by environment", yaml: "auth:\n  registrationEnabled: true", override: "false"},
		{name: "invalid file value", yaml: "auth:\n  registrationEnabled: perhaps", wantError: true},
		{name: "invalid environment value", yaml: "{}", override: "perhaps", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			if tc.override != "" {
				t.Setenv("HYPERSERVER_AUTH_REGISTRATIONENABLED", tc.override)
			}
			cfg, err := loadTestConfig(t, tc.yaml)
			if tc.wantError {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), "registrationenabled") {
					t.Fatalf("error = %v, want registrationEnabled error", err)
				}
				return
			}
			if err != nil || cfg.Auth.RegistrationEnabled != tc.want {
				t.Fatalf("registrationEnabled = %t, error = %v", cfg.Auth.RegistrationEnabled, err)
			}
		})
	}
}

func TestMailTimeoutConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, override string
		want                 time.Duration
		wantErr              bool
	}{
		{name: "omitted", yaml: "{}"},
		{name: "file", yaml: "mail:\n  timeout: 12s\n", want: 12 * time.Second},
		{name: "environment override", yaml: "mail:\n  timeout: 12s\n", override: "2s", want: 2 * time.Second},
		{name: "environment only", yaml: "{}", override: "3s", want: 3 * time.Second},
		{name: "malformed", yaml: "mail:\n  timeout: tomorrow\n", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			if tc.override != "" {
				t.Setenv("HYPERSERVER_MAIL_TIMEOUT", tc.override)
			}
			cfg, err := loadTestConfig(t, tc.yaml)
			if (err != nil) != tc.wantErr || cfg.Mail.Timeout != tc.want {
				t.Fatalf("timeout=%v, error=%v, want %v, error=%v", cfg.Mail.Timeout, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestResetMinimumResponseTimeConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, override string
		want                 time.Duration
		wantError            bool
	}{
		{name: "default", yaml: "{}", want: 2 * time.Second},
		{name: "file", yaml: "auth:\n  resetMinimumResponseTime: 750ms", want: 750 * time.Millisecond},
		{name: "zero duration", yaml: "auth:\n  resetMinimumResponseTime: 0s"},
		{name: "numeric zero", yaml: "auth:\n  resetMinimumResponseTime: 0"},
		{name: "environment only", yaml: "{}", override: "3s", want: 3 * time.Second},
		{name: "environment override", yaml: "auth:\n  resetMinimumResponseTime: 4s", override: "500ms", want: 500 * time.Millisecond},
		{name: "environment disables wait", yaml: "auth:\n  resetMinimumResponseTime: 4s", override: "0"},
		{name: "negative file", yaml: "auth:\n  resetMinimumResponseTime: -1ms", wantError: true},
		{name: "negative environment", yaml: "{}", override: "-1s", wantError: true},
		{name: "malformed file", yaml: "auth:\n  resetMinimumResponseTime: tomorrow", wantError: true},
		{name: "malformed environment", yaml: "{}", override: "tomorrow", wantError: true},
		{name: "overflow", yaml: "{}", override: "999999999999999999999h", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			if tc.override != "" {
				t.Setenv("HYPERSERVER_AUTH_RESETMINIMUMRESPONSETIME", tc.override)
			}
			cfg, err := loadTestConfig(t, tc.yaml)
			if tc.wantError {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), "resetminimumresponsetime") {
					t.Fatalf("error = %v, want resetMinimumResponseTime error", err)
				}
				return
			}
			if err != nil || cfg.Auth.ResetMinimumResponseTime != tc.want {
				t.Fatalf("minimum response time = %v, error = %v, want %v", cfg.Auth.ResetMinimumResponseTime, err, tc.want)
			}
		})
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
		{"negative reset response time", func(c *Config) { c.Auth.ResetMinimumResponseTime = -time.Nanosecond }, "auth.resetMinimumResponseTime"},
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
	template, err := os.ReadFile("../config-template.yaml")
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
			t.Chdir(dir)
			cfg, err := GetConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTP.Session.JwtKey != tc.want {
				t.Fatal("session signing key did not load from the expected source")
			}
		})
	}
}
