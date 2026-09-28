package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestTemplateSessionSigningKey(t *testing.T) {
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
			// GetConfig currently uses global Viper state. Keep these tests serial.
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Setenv("HYPERSERVER_HTTP_SESSION_JWTKEY", tc.environmentKey)
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0600); err != nil {
				t.Fatal(err)
			}
			viper.AddConfigPath(dir)
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
