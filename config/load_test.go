package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConfigFileSearch(t *testing.T) {
	for _, location := range []string{".", "config", "../config", "../../config"} {
		t.Run(location, func(t *testing.T) {
			cleanConfigEnvironment(t)
			working := filepath.Join(t.TempDir(), "a", "b", "c")
			if err := os.MkdirAll(working, 0700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(working)
			path := filepath.Join(location, "config.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("app:\n  name: selected"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := GetConfig()
			if err != nil || cfg.App.Name != "selected" {
				t.Fatalf("selected config = %q, error = %v", cfg.App.Name, err)
			}

			// A nearer file takes precedence. An invalid nearer file must not fall back.
			for _, data := range []string{"app:\n  name: nearest", "invalid: ["} {
				if err := os.WriteFile("config.yaml", []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				cfg, err = GetConfig()
				if data == "invalid: [" {
					if err == nil {
						t.Fatal("invalid first file fell back to another file")
					}
				} else if err != nil || cfg.App.Name != "nearest" {
					t.Fatalf("nearest config = %q, error = %v", cfg.App.Name, err)
				}
			}
		})
	}
}

func TestConfigFileRequired(t *testing.T) {
	cleanConfigEnvironment(t)
	working := filepath.Join(t.TempDir(), "a", "b", "c")
	if err := os.MkdirAll(working, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(working)
	t.Setenv("HYPERSERVER_APP_NAME", "environment alone is not enough")
	if _, err := GetConfig(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error = %v", err)
	}
	if err := os.Mkdir("config.yaml", 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := GetConfig(); err == nil {
		t.Fatal("directory accepted as a configuration file")
	}
}

func TestInitializationTimeoutConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, file, env string
		want            time.Duration
	}{
		{name: "default", file: "{}", want: 10 * time.Second},
		{name: "file", file: "app:\n  initializationTimeout: 2m", want: 2 * time.Minute},
		{name: "environment", file: "{}", env: "45s", want: 45 * time.Second},
		{name: "override", file: "app:\n  initializationTimeout: 2m", env: "30s", want: 30 * time.Second},
		{name: "subsecond", file: "app:\n  initializationTimeout: 500ms", want: 500 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			var environment []string
			if test.env != "" {
				environment = []string{"HYPERSERVER_APP_INITIALIZATIONTIMEOUT=" + test.env}
			}
			cfg, err := readConfig(strings.NewReader(test.file), environment)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.App.InitializationTimeout == nil || *cfg.App.InitializationTimeout != test.want {
				t.Fatalf("initialization timeout = %v, want %v", cfg.App.InitializationTimeout, test.want)
			}
		})
	}
	for _, value := range []string{"0", "0s", "-1s", "12", "not-a-duration", "null", "999999999999999999h", "\"\"", ""} {
		for _, environment := range []bool{false, true} {
			t.Run(value+map[bool]string{false: "/file", true: "/environment"}[environment], func(t *testing.T) {
				file := "app:\n  initializationTimeout: " + value
				var env []string
				if environment {
					file = "app:\n  initializationTimeout: 1m"
					env = []string{"HYPERSERVER_APP_INITIALIZATIONTIMEOUT=" + value}
				}
				_, err := readConfig(strings.NewReader(file), env)
				if err == nil || !strings.Contains(err.Error(), "app.initializationTimeout") {
					t.Fatalf("invalid timeout error = %v", err)
				}
			})
		}
	}
	if err := (Config{}).Validate(); err != nil {
		t.Fatal("omitted programmatic setting must remain valid:", err)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, file, environment, want string
	}{
		{"malformed YAML", "mail: [", "", "invalid YAML"},
		{"multiple documents", "{}\n---\n{}", "", "one YAML document"},
		{"duplicate keys", "app:\n  name: first\n  name: second", "", "unique keys"},
		{"case duplicate", "app:\n  name: first\n  Name: second", "", "duplicate"},
		{"non-string nested key", "http:\n  1: invalid", "", "setting names must be strings"},
		{"recursive alias", "app: &loop\n  name: *loop", "", "YAML mapping"},
		{"scalar document", "secret-marker", "", "YAML mapping"},
		{"list document", "[secret-marker]", "", "YAML mapping"},
		{"null document", "null", "", "YAML mapping"},
		{"null field", "http:\n  port: null", "", "http.port"},
		{"unknown field", "http:\n  prt: 8080", "", "http.prt"},
		{"unknown empty section", "unexpected: {}", "", "unexpected"},
		{"unknown environment", "{}", "HYPERSERVER_HTTP_PRT=8080", "http.prt"},
		{"scalar section", "http: secret-marker", "", "http"},
		{"mapping scalar", "http:\n  port: {}", "", "http.port"},
		{"list scalar", "mail:\n  password: [secret-marker]", "", "mail.password"},
		{"nested provider option", "auth:\n  accountStorage:\n    options:\n      connection:\n        secret-marker: value", "", "auth.accountstorage.options.connection"},
		{"invalid boolean", "auth:\n  enabled: secret-marker", "", "auth.enabled"},
		{"invalid integer", "http:\n  port: secret-marker", "", "http.port"},
		{"invalid environment integer", "{}", "HYPERSERVER_HTTP_PORT=secret-marker", "http.port"},
		{"invalid duration", "mail:\n  timeout: secret-marker", "", "mail.timeout"},
		{"numeric duration requires units", "mail:\n  timeout: 10", "", "mail.timeout"},
		{"empty numeric override", "http:\n  port: 8080", "HYPERSERVER_HTTP_PORT=", "http.port"},
		{"fractional integer", "http:\n  port: 80.5", "", "http.port"},
		{"negative unsigned", "http:\n  port: -1", "", "http.port"},
		{"unsigned overflow", "mail:\n  port: 65536", "", "mail.port"},
		{"signed overflow", "http:\n  hstsMaxAge: 9223372036854775808", "", "http.hstsMaxAge"},
		{"environment overflow", "{}", "HYPERSERVER_HTTP_PORT=65536", "http.port"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var environment []string
			if tc.environment != "" {
				environment = []string{tc.environment}
			}
			cfg, err := readConfig(strings.NewReader(tc.file), environment)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "secret-marker") {
				t.Fatal("configuration error exposed a value")
			}
			if !reflect.DeepEqual(cfg, Config{}) {
				t.Fatal("failed load returned partial configuration")
			}
		})
	}
}

func TestConfigPrecedenceAndProviderValues(t *testing.T) {
	const file = `HTTP:
  listenHost: localhost
  port: invalid-overridden-value
  session:
    stores:
      custom:
        enabled: true
        connection: "file=secret#value"
        attempts: 3
    types:
      auth-user-session: custom
auth:
  accountStorage:
    provider: CloudProvider
    options:
      bucket_name: CaseSensitive
  services:
    custom:
      enabled: false
mail:
  password: file-secret
`
	cfg, err := readConfig(strings.NewReader(file), []string{
		"UNRELATED=value",
		"HYPERSERVER_HTTP_PORT=65535",
		"HYPERSERVER_HTTP_SESSION_STORES_CUSTOM_ENABLED=false",
		"HYPERSERVER_HTTP_SESSION_STORES_CUSTOM_CONNECTION=",
		"HYPERSERVER_HTTP_SESSION_STORES_CUSTOM_NEWOPTION=env-secret#value: []",
		"HYPERSERVER_AUTH_SERVICES_CUSTOM_ENABLED=true",
		"HYPERSERVER_MAIL_PASSWORD=",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.ListenHost != "localhost" || cfg.HTTP.Port != 65535 || cfg.Mail.Password != "" || cfg.Auth.ResetMinimumResponseTime != 2*time.Second {
		t.Fatal("file, environment, and default precedence changed")
	}
	want := map[string]string{"enabled": "false", "connection": "", "attempts": "3", "newoption": "env-secret#value: []"}
	if !reflect.DeepEqual(cfg.HTTP.Session.Stores["custom"], want) || cfg.HTTP.Session.Types["auth-user-session"] != "custom" {
		t.Fatal("provider values or session names changed")
	}
	if cfg.Auth.AccountStorage.Provider != "CloudProvider" || cfg.Auth.AccountStorage.Options["bucket_name"] != "CaseSensitive" || cfg.Auth.Services["custom"]["enabled"] != "true" {
		t.Fatal("provider configuration changed")
	}
}

func TestConfigYAMLAliases(t *testing.T) {
	cfg, err := readConfig(strings.NewReader("http:\n  defaultRateLimit: &policy\n    requests: 35\n    window: 2m\n  sharedRateLimit:\n    <<: *policy\n    requests: 50\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.DefaultRateLimit.Requests != 35 || cfg.HTTP.SharedRateLimit.Requests != 50 || cfg.HTTP.SharedRateLimit.Window != 2*time.Minute {
		t.Fatal("YAML merge or alias values were not loaded")
	}
}

func TestEmptyConfiguration(t *testing.T) {
	for _, file := range []string{"", "# no settings\n", "{}"} {
		cfg, err := readConfig(strings.NewReader(file), nil)
		if err != nil || !reflect.DeepEqual(cfg, defaultConfig()) {
			t.Fatalf("empty configuration did not retain defaults: %v", err)
		}
	}
	cfg, err := readConfig(strings.NewReader("auth:\n  services:\n    custom: {}"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if options, exists := cfg.Auth.Services["custom"]; !exists || len(options) != 0 {
		t.Fatal("empty provider configuration was dropped")
	}
}

// Guard the explicit field list when new configuration fields are introduced.
func TestConfigFieldsCoverScalarAndListSettings(t *testing.T) {
	cfg := defaultConfig()
	fields := configFields(&cfg)
	var visit func(reflect.Value, string)
	visit = func(value reflect.Value, path string) {
		if value.Kind() == reflect.Pointer {
			value = value.Elem()
		}
		for i := 0; i < value.NumField(); i++ {
			field := value.Field(i)
			name := path + value.Type().Field(i).Name
			if field.Kind() == reflect.Pointer {
				if field.IsNil() {
					t.Fatalf("%s has no default configuration value", name)
				}
				field = field.Elem()
			}
			switch field.Kind() {
			case reflect.Struct:
				visit(field, name+".")
			case reflect.Map:
				// Provider-owned keys are exercised separately.
			default:
				found := false
				for key, destination := range fields {
					if strings.EqualFold(key, name) {
						found = destination == field.Addr().Interface()
						delete(fields, key)
						break
					}
				}
				if !found {
					t.Errorf("%s has no correctly bound configuration field", name)
				}
			}
		}
	}
	visit(reflect.ValueOf(&cfg).Elem(), "")
	if len(fields) != 0 {
		t.Errorf("unexpected fields: %v", fields)
	}
}

func TestConcurrentConfigLoads(t *testing.T) {
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Go(func() {
			cfg, err := readConfig(strings.NewReader("{}"), []string{"HYPERSERVER_HTTP_SESSION_STORES_CUSTOM_ENABLED=true"})
			if err != nil {
				t.Error(err)
				return
			}
			if cfg.Auth.RateLimit.MaxClients != 4096 || cfg.HTTP.Session.Stores["custom"]["enabled"] != "true" {
				t.Error("configuration leaked between concurrent loads")
			}
			cfg.Auth.RateLimit.MaxClients = 1
			cfg.HTTP.Session.Stores["custom"]["enabled"] = "false"
		})
	}
	workers.Wait()
}
