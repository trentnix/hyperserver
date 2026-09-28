// config.go defines the application configuration object, which is loaded from config.yaml using viper
//
// All configuration values can be overridden by environment variables. The name of the variable is
// determined by the set prefix and the name of the configuration field in config/config.yaml.
//
// In config/config.go, the prefix is set as hyperserver via viper.SetEnvPrefix("hyperserver"). Nested fields require
// an underscore between levels. For example:
//
//	 http:
//	   port: 1234
//
//	can be overridden by setting an environment variable with the name HYPERSERVER_HTTP_PORT.
//
// It is good practice to override values that need to be secure with an environment variable that
// is loaded securely.
package config

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type (
	// Config stores complete configuration
	Config struct {
		HTTP     HTTPConfig
		Database DatabaseConfig
		App      AppConfig
		Auth     AuthConfig
		Mail     MailConfig
	}

	// HTTPConfig stores HTTP configuration
	HTTPConfig struct {
		Hostname     string
		Port         uint16
		ReadTimeout  time.Duration
		WriteTimeout time.Duration
		IdleTimeout  time.Duration
		TLS          struct {
			Enabled     bool
			Certificate string
			Key         string
		}
		Session struct {
			JwtKey    string
			TokenAge  time.Duration
			CookieAge time.Duration
			Stores    map[string]map[string]string `mapstructure:"stores"`
			Types     map[string]string            `mapstructure:"types"`
		}
	}

	AuthConfig struct {
		Enabled                      bool
		JwtKey                       string
		VerificationEndpoint         string
		VerificationTokenExpiration  time.Duration
		ResetTokenExpiration         time.Duration
		ResetRequiresNewCredentials  bool
		RegisterRequiresVerification bool
		Services                     map[string]map[string]string `mapstructure:"services"`
	}

	// DatabaseConfig stores the database configuration
	DatabaseConfig struct {
		Driver         string
		Connection     string
		TestConnection string
	}

	// AppConfig stores application configuration
	AppConfig struct {
		Name                string
		WorkingDirectory    string
		RenderNotifications bool
	}

	// MailConfig stores the mail configuration.
	MailConfig struct {
		Hostname    string
		Port        uint16
		User        string
		Password    string
		FromAddress string
	}
)

// GetConfig loads and reads the application configuration file
func GetConfig() (Config, error) {
	v := viper.New()

	// Load the config file - config.yaml in either the current folder or
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("config")
	v.AddConfigPath("../config")
	v.AddConfigPath("../../config")
	return readConfig(v)
}

func readConfig(v *viper.Viper) (Config, error) {
	var c Config
	v.SetEnvPrefix("hyperserver")
	v.AutomaticEnv()
	v.AllowEmptyEnv(true)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Binding environment keys makes omitted YAML fields visible to Unmarshal,
	// including provider options. Empty overrides must not revive file secrets.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if suffix, ok := strings.CutPrefix(name, "HYPERSERVER_"); ok {
			key := strings.ToLower(strings.ReplaceAll(suffix, "_", "."))
			if err := v.BindEnv(key, name); err != nil {
				return c, err
			}
		}
	}

	if err := v.ReadInConfig(); err != nil {
		return c, err
	}
	if err := v.Unmarshal(&c); err != nil {
		return c, err
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}
