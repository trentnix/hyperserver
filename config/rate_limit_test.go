package config

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRateLimitConfiguration(t *testing.T) {
	for _, scope := range []string{"sharedRateLimit", "defaultRateLimit"} {
		for _, tc := range []struct {
			name, yaml string
			env        map[string]string
			want       RateLimitConfig
			wantErr    string
		}{
			{name: "omitted", yaml: "{}", want: RateLimitConfig{Requests: 120, Window: time.Minute, MaxClients: 4096}},
			{name: "enabled defaults", yaml: "http:\n  sharedRateLimit:\n    enabled: true", want: RateLimitConfig{Enabled: true, Requests: 120, Window: time.Minute, MaxClients: 4096}},
			{name: "file values", yaml: "http:\n  sharedRateLimit:\n    enabled: true\n    requests: 30\n    window: 2m\n    maxClients: 200", want: RateLimitConfig{Enabled: true, Requests: 30, Window: 2 * time.Minute, MaxClients: 200}},
			{name: "environment only", yaml: "{}", env: map[string]string{
				"HYPERSERVER_HTTP_SHAREDRATELIMIT_ENABLED": "true", "HYPERSERVER_HTTP_SHAREDRATELIMIT_REQUESTS": "40",
				"HYPERSERVER_HTTP_SHAREDRATELIMIT_WINDOW": "3m", "HYPERSERVER_HTTP_SHAREDRATELIMIT_MAXCLIENTS": "300",
			}, want: RateLimitConfig{Enabled: true, Requests: 40, Window: 3 * time.Minute, MaxClients: 300}},
			{name: "environment disables file", yaml: "http:\n  sharedRateLimit:\n    enabled: true\n    requests: 30", env: map[string]string{"HYPERSERVER_HTTP_SHAREDRATELIMIT_ENABLED": "false"}, want: RateLimitConfig{Requests: 30, Window: time.Minute, MaxClients: 4096}},
			{name: "environment overrides budget", yaml: "http:\n  sharedRateLimit:\n    enabled: true\n    requests: 30", env: map[string]string{"HYPERSERVER_HTTP_SHAREDRATELIMIT_REQUESTS": "50"}, want: RateLimitConfig{Enabled: true, Requests: 50, Window: time.Minute, MaxClients: 4096}},
			{name: "zero requests", yaml: "http:\n  sharedRateLimit:\n    enabled: true\n    requests: 0", wantErr: "http.sharedRateLimit.requests"},
			{name: "negative requests", yaml: "http:\n  sharedRateLimit:\n    enabled: true\n    requests: -1", wantErr: "http.sharedRateLimit.requests"},
			{name: "zero window", yaml: "http:\n  sharedRateLimit:\n    enabled: true\n    window: 0s", wantErr: "http.sharedRateLimit.window"},
			{name: "negative window", yaml: "http:\n  sharedRateLimit:\n    enabled: true\n    window: -1s", wantErr: "http.sharedRateLimit.window"},
			{name: "zero capacity", yaml: "http:\n  sharedRateLimit:\n    enabled: true\n    maxClients: 0", wantErr: "http.sharedRateLimit.maxClients"},
			{name: "negative capacity", yaml: "http:\n  sharedRateLimit:\n    enabled: true\n    maxClients: -1", wantErr: "http.sharedRateLimit.maxClients"},
			{name: "invalid duration", yaml: "http:\n  sharedRateLimit:\n    enabled: true\n    window: tomorrow", wantErr: "invalid duration"},
			{name: "disabled needs no budget", yaml: "http:\n  sharedRateLimit:\n    requests: 0\n    window: 0s\n    maxClients: 0", want: RateLimitConfig{}},
		} {
			t.Run(scope+"/"+tc.name, func(t *testing.T) {
				cleanConfigEnvironment(t)
				for key, value := range tc.env {
					t.Setenv(strings.ReplaceAll(key, "SHAREDRATELIMIT", strings.ToUpper(scope)), value)
				}
				cfg, err := loadTestConfig(t, strings.ReplaceAll(tc.yaml, "sharedRateLimit", scope))
				if tc.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), strings.ReplaceAll(tc.wantErr, "sharedRateLimit", scope)) {
						t.Fatalf("error=%v, want %q", err, tc.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				got, want := cfg.HTTP.SharedRateLimit, tc.want
				if scope == "defaultRateLimit" {
					got = cfg.HTTP.DefaultRateLimit
					if tc.name == "omitted" || tc.name == "enabled defaults" {
						want.Requests = 20
					}
				}
				if got != want {
					t.Fatalf("limit=%+v, want %+v", got, want)
				}
			})
		}
	}
}

func TestAmbiguousRateLimitConfigurationRejected(t *testing.T) {
	for _, source := range []string{"file", "environment"} {
		t.Run(source, func(t *testing.T) {
			cleanConfigEnvironment(t)
			yaml := "http:\n  rateLimit:\n    enabled: true"
			if source == "environment" {
				yaml = "{}"
				t.Setenv("HYPERSERVER_HTTP_RATELIMIT_ENABLED", "false")
			}
			if _, err := loadTestConfig(t, yaml); err == nil || !strings.Contains(err.Error(), "http.rateLimit is ambiguous") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestRateLimitSettingsAreIndependent(t *testing.T) {
	cleanConfigEnvironment(t)
	cfg, err := loadTestConfig(t, "http:\n  defaultRateLimit:\n    enabled: true\n    requests: 2\n  sharedRateLimit:\n    enabled: true\n    requests: 30\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.DefaultRateLimit.Requests != 2 || cfg.HTTP.SharedRateLimit.Requests != 30 {
		t.Fatal("settings were merged")
	}
}

func TestModuleRateLimitConfiguration(t *testing.T) {
	for _, scope := range []struct{ section, key, env string }{
		{"auth", "rateLimit", "HYPERSERVER_AUTH_RATELIMIT_MAXCLIENTS"},
		{"app", "siteRateLimit", "HYPERSERVER_APP_SITERATELIMIT_MAXCLIENTS"},
	} {
		for _, tc := range []struct {
			name, file, env string
			want            int
			invalid         bool
		}{
			{name: "omitted", want: 4096},
			{name: "file", file: "10000", want: 10000},
			{name: "environment only", env: "5000", want: 5000},
			{name: "environment overrides file", file: "10000", env: "2000", want: 2000},
			{name: "zero file", file: "0", invalid: true},
			{name: "negative file", file: "-1", invalid: true},
			{name: "zero environment", file: "10000", env: "0", invalid: true},
			{name: "negative environment", env: "-1", invalid: true},
			{name: "malformed file", file: "invalid", invalid: true},
			{name: "malformed environment", env: "invalid", invalid: true},
		} {
			t.Run(scope.section+"/"+tc.name, func(t *testing.T) {
				cleanConfigEnvironment(t)
				yaml := "{}"
				if tc.file != "" {
					yaml = fmt.Sprintf("%s:\n  %s:\n    maxClients: %s", scope.section, scope.key, tc.file)
				}
				if tc.env != "" {
					t.Setenv(scope.env, tc.env)
				}
				cfg, err := loadTestConfig(t, yaml)
				if tc.invalid {
					if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(scope.section+"."+scope.key+".maxClients")) {
						t.Fatalf("error = %v, want invalid module capacity", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				configured, other := cfg.Auth.RateLimit, cfg.App.SiteRateLimit
				if scope.section == "app" {
					configured, other = other, configured
				}
				if configured == nil || configured.MaxClients != tc.want || other == nil || other.MaxClients != 4096 {
					t.Fatalf("configured = %+v, other = %+v, want %d and 4096", configured, other, tc.want)
				}
				if cfg.HTTP.DefaultRateLimit.MaxClients != 4096 || cfg.HTTP.SharedRateLimit.MaxClients != 4096 {
					t.Fatal("module capacity changed an HTTP capacity")
				}
			})
		}
	}
}

func TestModuleRateLimitProgrammaticConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *ModuleRateLimitConfig
		want int
	}{
		{"default", nil, 4096},
		{"configured", &ModuleRateLimitConfig{MaxClients: 10000}, 10000},
		{"zero", &ModuleRateLimitConfig{}, 0},
		{"negative", &ModuleRateLimitConfig{MaxClients: -1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.cfg.ClientCapacity()
			if got != tc.want || (err != nil) != (tc.want == 0) {
				t.Fatalf("capacity = %d, error = %v", got, err)
			}
		})
	}
}
