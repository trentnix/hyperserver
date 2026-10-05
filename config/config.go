// Package config loads and validates HyperServer configuration.
// GetConfig searches for config.yaml in the working directory and nearby config
// directories. HYPERSERVER_ environment variables override file values, with
// underscores separating nested keys, for example HYPERSERVER_HTTP_PORT.
// Provider packages validate their own configuration.
package config

import (
	"errors"
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
		PublicOrigin     string          // Address for absolute links. Required with trusted proxies.
		TrustedProxies   []string        // Direct proxy peer CIDRs. Empty trusts no forwarding headers.
		HSTSMaxAge       int64           // HTTPS-only HSTS lifetime in seconds. Zero disables the header.
		ListenHost       string          // Host or unbracketed IP address. Empty defaults to 127.0.0.1.
		Port             uint16          // Listener port. Zero requests an ephemeral port but cannot supply fallback links.
		ReadTimeout      time.Duration   // Maximum time to read a request, including its body.
		WriteTimeout     time.Duration   // Maximum time to write a response.
		IdleTimeout      time.Duration   // Maximum wait for another request on a keep-alive connection.
		DefaultRateLimit RateLimitConfig // Inherited per-route policy. Module and handler policies replace it.
		SharedRateLimit  RateLimitConfig // Optional aggregate per-client budget. Route overrides do not bypass it.
		// TLS enables direct HTTPS using a PEM certificate and private key.
		TLS struct {
			Enabled     bool
			Certificate string
			Key         string
		}
		// Session contains signing settings, provider options, and named-store selections.
		Session struct {
			JwtKey    string
			TokenAge  time.Duration
			CookieAge time.Duration
			Stores    map[string]map[string]string `mapstructure:"stores"`
			Types     map[string]string            `mapstructure:"types"`
		}
	}

	// RateLimitConfig specifies an optional per-client policy or shared budget.
	RateLimitConfig struct {
		Enabled    bool          // Enables this policy or budget. Defaults to false.
		Requests   int           // Requests allowed per client IP in each window.
		Window     time.Duration // Window duration, starting with the client's first request.
		MaxClients int           // Maximum tracked IPs. New clients are rejected at capacity.
	}

	// AuthConfig selects authentication providers, registration policy, and account-token settings.
	AuthConfig struct {
		RateLimit                    *ModuleRateLimitConfig // Capacity for each authentication mutation route. Nil uses defaults.
		Enabled                      bool                   // Enables authentication routes and provider initialization.
		RegistrationEnabled          bool                   // Allows new accounts. Defaults to false without disabling existing-account recovery.
		JwtKey                       string                 // Signs account tokens. Must differ from the session signing key.
		VerificationEndpoint         string
		VerificationTokenExpiration  time.Duration
		ResetTokenExpiration         time.Duration
		ResetMinimumResponseTime     time.Duration // Minimum reset response time. File loading defaults to 2s. Zero disables the wait.
		ResetRequiresNewCredentials  bool
		RegisterRequiresVerification bool                         // Requires configured verification delivery when registration is enabled.
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
		SiteRateLimit       *ModuleRateLimitConfig // Capacity for each reference-site submission route. Nil uses defaults.
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
		Timeout     time.Duration // Total SMTP operation limit. Zero uses 30 seconds.
	}
)

// GetConfig loads config.yaml, applies environment overrides, and validates shared settings.
// It searches ., config, ../config, and ../../config in that order. Provider-specific
// validation must run separately. A missing or invalid file returns an error.
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
	v.SetDefault("http.listenHost", "127.0.0.1")
	v.SetDefault("http.defaultRateLimit.requests", 20)
	v.SetDefault("http.defaultRateLimit.window", time.Minute)
	v.SetDefault("http.defaultRateLimit.maxClients", 4096)
	v.SetDefault("http.sharedRateLimit.requests", 120)
	v.SetDefault("http.sharedRateLimit.window", time.Minute)
	v.SetDefault("http.sharedRateLimit.maxClients", 4096)
	v.SetDefault("auth.rateLimit.maxClients", 4096)
	v.SetDefault("app.siteRateLimit.maxClients", 4096)
	v.SetDefault("auth.resetMinimumResponseTime", 2*time.Second)

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
	for _, key := range v.AllKeys() {
		if key == "http.ratelimit" || strings.HasPrefix(key, "http.ratelimit.") {
			return c, errors.New("http.rateLimit is ambiguous. Use http.defaultRateLimit for inherited settings or http.sharedRateLimit for an aggregate budget")
		}
	}
	if v.InConfig("http.hostname") || v.IsSet("http.hostname") {
		return c, errors.New("http.hostname was renamed to http.listenHost. Rename HYPERSERVER_HTTP_HOSTNAME to HYPERSERVER_HTTP_LISTENHOST for environment configuration")
	}
	if err := v.Unmarshal(&c); err != nil {
		return c, err
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}
