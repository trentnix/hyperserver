// Package config loads and validates HyperServer configuration.
// GetConfig searches for config.yaml in the working directory and nearby config
// directories. HYPERSERVER_ environment variables override file values, with
// underscores separating nested keys, for example HYPERSERVER_HTTP_PORT.
// Provider packages validate their own configuration.
package config

import "time"

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
			Stores    map[string]map[string]string
			Types     map[string]string
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
		AccountStorage               AccountStorageConfig   // Selects account persistence, separate from authentication services.
		RateLimit                    *ModuleRateLimitConfig // Capacity for each authentication mutation route. Nil uses defaults.
		Enabled                      bool                   // Enables authentication routes and provider initialization.
		RegistrationEnabled          bool                   // Allows new accounts. Defaults to false without disabling existing-account recovery.
		JwtKey                       string                 // Signs account tokens. Must differ from the session signing key.
		VerificationEndpoint         string
		VerificationTokenExpiration  time.Duration
		ResetTokenExpiration         time.Duration
		ResetMinimumResponseTime     time.Duration // Minimum reset response time. File loading defaults to 2s. Zero disables the wait.
		ResetRequiresNewCredentials  bool
		RegisterRequiresVerification bool // Requires configured verification delivery when registration is enabled.
		Services                     map[string]map[string]string
	}

	// AccountStorageConfig selects one account repository provider and its options.
	AccountStorageConfig struct {
		Provider string            // Case-sensitive provider name. Empty selects sqlite.
		Options  map[string]string // Validated by the selected provider. SQLite accepts no options.
	}

	// DatabaseConfig stores the database configuration
	DatabaseConfig struct {
		Driver         string
		Connection     string
		TestConnection string
	}

	// AppConfig stores application configuration
	AppConfig struct {
		InitializationTimeout *time.Duration         // Module startup budget. Nil uses the initializer's default. If set, must be positive.
		SiteRateLimit         *ModuleRateLimitConfig // Capacity for each reference-site submission route. Nil uses defaults.
		Name                  string
		WorkingDirectory      string
		RenderNotifications   bool
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
