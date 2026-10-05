package auth

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// authServices is the array of AuthService instances that is used when an AuthService
// implementation self-registers via init()
var authServices []AuthService

// VerificationConfigValidator checks a provider's verification mechanism without I/O.
// Providers must implement it when registration requires verification. Validation
// runs after Init and must not send instructions or contact the delivery service.
type VerificationConfigValidator interface {
	ValidateVerification() error
}

// VerificationEmailSender is an optional capability of an authentication provider.
// SendVerificationEmail sends instructions to the account's stored email address.
// origin supplies the scheme and host from application settings and connection
// metadata, not request headers. Callers resolve the application's public origin.
type VerificationEmailSender interface {
	SendVerificationEmail(context.Context, *user.User, url.URL) error
}

// AuthService provides the HTTP steps for authentication and account management.
// Providers write form errors themselves and return operation results to AuthManager.
type AuthService interface {
	// Routes binds any provider-specific routes after initialization.
	// Registration errors must stop startup before accepting traffic.
	Routes(*routing.Routes) error
	// Init configures the provider using application-owned services.
	Init(*server.ApplicationServer) error
	// IsLoaded reports whether the provider is available for selection.
	IsLoaded() bool
	// AuthType returns the provider's route and configuration identifier.
	AuthType() string

	// GetLoginButton returns trusted HTML linking to the provider's login form.
	GetLoginButton() template.HTML
	// GetRegisterButton returns trusted HTML linking to registration, or an empty value when unavailable.
	GetRegisterButton() template.HTML

	// GetLogin renders the provider's login form.
	GetLogin(http.ResponseWriter, *http.Request)
	// Login authenticates submitted credentials and returns the account, or nil on failure.
	Login(http.ResponseWriter, *http.Request) *user.User

	// GetRegister renders the provider's registration form.
	GetRegister(http.ResponseWriter, *http.Request)
	// Register processes registration and reports whether the handler can continue with success.
	// A false result does not guarantee that no account was created.
	Register(http.ResponseWriter, *http.Request) bool

	// GetResetRequest renders the recovery-request form.
	GetResetRequest(http.ResponseWriter, *http.Request)
	// ResetRequest must give valid submissions the same HTTP acknowledgment for
	// known, unknown, and ineligible accounts, including storage or delivery failures.
	// The returned result is internal and must not change that acknowledgment.
	ResetRequest(w http.ResponseWriter, r *http.Request, tokenExpiration time.Duration) bool
	// GetReset renders the reset form after AuthManager validates token.
	GetReset(w http.ResponseWriter, r *http.Request, token string)
	// Reset must revalidate and consume the exact token atomically with the credential
	// change. AuthManager's earlier validation does not prevent concurrent reuse.
	// Return true only after both changes commit.
	Reset(w http.ResponseWriter, r *http.Request, u *user.User, token string, requireNewCredentials bool) bool

	// GetChange renders the credential-change form for the authenticated account.
	GetChange(w http.ResponseWriter, r *http.Request)
	// Change checks existing credentials and applies their replacement.
	Change(w http.ResponseWriter, r *http.Request, u *user.User) bool
}

// Register appends a shared provider instance to the process-wide registry.
// It does not initialize the provider. Call it during startup, not concurrently with requests.
func Register(a AuthService) {
	authServices = append(authServices, a)
}

// GetAuthServices returns the registry's backing slice, not a copy.
// Callers must not modify it while the application is running.
func GetAuthServices() []AuthService {
	return authServices
}

// RemoveAuthService removes any AuthService implementations from the registered
// AuthServices that matches the specified authType. This is to deal with
// situations where the AuthService could not be initialized or is disabled.
func RemoveAuthService(authType string) []AuthService {
	i := 0
	for _, service := range authServices {
		if service.AuthType() != authType {
			authServices[i] = service
			i++
		}
	}
	// Slice off the removed elements.
	authServices = authServices[:i]
	return authServices
}

// GetLoadedAuthServices returns registered providers whose IsLoaded method reports true.
func GetLoadedAuthServices() []AuthService {
	var loadedAuthServices []AuthService
	for _, s := range authServices {
		if s.IsLoaded() {
			loadedAuthServices = append(loadedAuthServices, s)
		}
	}

	return loadedAuthServices
}

// GetAuthConfigOptions retrieves the configuration options map for the specified
// authType
func GetAuthConfigOptions(c *config.Config, authType string) map[string]string {
	for configOption, configData := range c.Auth.Services {
		if strings.EqualFold(configOption, authType) {
			return configData
		}
	}

	return nil
}
