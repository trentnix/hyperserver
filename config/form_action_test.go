package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestFormActionOrigins(t *testing.T) {
	for _, value := range []string{"", "*", "https://*.example", "https://example;script-src", "https://example,other", "https://example 'unsafe-inline'", "https://user:secret@example", "https://example/path", "https://example?query", "https://example#fragment", "javascript:alert(1)", "https://example\\evil", "https://example\n"} {
		cfg := Config{HTTP: HTTPConfig{FormActionOrigins: []string{value}}}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "http.formActionOrigins[0]") {
			t.Errorf("origin %q: %v", value, err)
		}
	}
	cfg := HTTPConfig{FormActionOrigins: []string{"https://login.example/", "http://127.0.0.1:8080", "https://[::1]:443"}}
	got, err := cfg.FormActionSources()
	if err != nil || !reflect.DeepEqual(got, []string{"https://login.example", "http://127.0.0.1:8080", "https://[::1]:443"}) {
		t.Fatalf("sources=%v error=%v", got, err)
	}
}

func TestFormActionConfiguration(t *testing.T) {
	for _, override := range []string{"", "https://other.example,https://another.example"} {
		cleanConfigEnvironment(t)
		if override != "" {
			t.Setenv("HYPERSERVER_HTTP_FORMACTIONORIGINS", override)
		}
		cfg, err := loadTestConfig(t, "http:\n  formActionOrigins: [https://login.example]\n")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"https://login.example"}
		if override != "" {
			want = strings.Split(override, ",")
		}
		if !reflect.DeepEqual(cfg.HTTP.FormActionOrigins, want) {
			t.Fatalf("origins=%v", cfg.HTTP.FormActionOrigins)
		}
	}
}
