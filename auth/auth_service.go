package auth

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

var catalog struct {
	sync.RWMutex
	descriptors []Descriptor
}

// Descriptor names an authentication provider and supplies its factory.
// New must return a fresh instance without I/O. Name must match AuthType.
type Descriptor struct {
	Name string
	New  func() AuthService
}

// Registry holds one application's enabled providers. Initialize them before
// serving requests. A registry must not be shared between applications.
type Registry struct {
	services []AuthService
}

// NewRegistry validates provider selections and creates enabled instances only.
// The catalog can be an import snapshot or an application-supplied local catalog.
func NewRegistry(c *config.Config, descriptors []Descriptor) (*Registry, error) {
	if err := ValidateConfig(c, descriptors); err != nil {
		return nil, err
	}
	r := &Registry{}
	if !c.Auth.Enabled {
		return r, nil
	}

	for _, d := range descriptors {
		enabled, err := config.ProviderEnabled("auth.services."+d.Name+".enabled", GetAuthConfigOptions(c, d.Name)["enabled"])
		if err != nil {
			return nil, err
		}
		if !enabled {
			continue
		}
		service := d.New()
		if service == nil || service.AuthType() != d.Name {
			return nil, fmt.Errorf("auth provider %q returned no instance or a different AuthType", d.Name)
		}
		r.services = append(r.services, service)
	}
	return r, nil
}

// Services returns a copy of the application's provider list, not new instances.
func (r *Registry) Services() []AuthService {
	if r == nil {
		return nil
	}
	return slices.Clone(r.services)
}

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

// AuthService identifies and initializes an authentication provider. Providers
// implement the HTTP capabilities they support separately.
type AuthService interface {
	// Init configures the provider using application-owned services.
	Init(*server.ApplicationServer) error
	// IsLoaded reports whether the provider is available for selection.
	IsLoaded() bool
	// AuthType returns the provider's route and configuration identifier.
	AuthType() string
}

// ProviderRoutes optionally binds provider-specific routes after initialization.
// Registration errors stop startup before accepting traffic.
type ProviderRoutes interface {
	Routes(*routing.Routes) error
}

// LoginHandler supplies login pages and credential submission handling.
// Login returns an account for the manager to establish its authenticated session.
type LoginHandler interface {
	AuthType() string
	// GetLoginButton returns trusted HTML linking to the provider's login form.
	GetLoginButton() template.HTML
	// GetLogin renders the provider's login form.
	GetLogin(http.ResponseWriter, *http.Request)
	// Login returns the authenticated account without committing response headers
	// or writing a body. The manager creates the session and writes the success redirect.
	// When Login returns nil, the provider must write the complete response,
	// including a failure response or a redirect that continues authentication.
	Login(http.ResponseWriter, *http.Request) *user.User
}

// RegistrationHandler supplies account registration. Providers without this
// capability are omitted from registration choices, even when registration is enabled.
type RegistrationHandler interface {
	AuthType() string
	// GetRegisterButton returns trusted HTML linking to registration.
	GetRegisterButton() template.HTML
	// GetRegister renders the provider's registration form.
	GetRegister(http.ResponseWriter, *http.Request)
	// Register processes registration and writes its response, including any redirect.
	// Delivery or rendering can fail after the account has been created.
	Register(http.ResponseWriter, *http.Request)
}

// ResetHandler supplies the request and redemption steps for credential recovery.
// The manager validates its reset token before delegating redemption.
type ResetHandler interface {
	// GetResetRequest renders the recovery-request form.
	GetResetRequest(http.ResponseWriter, *http.Request)
	// ResetRequest must give valid submissions the same HTTP acknowledgment for
	// known, unknown, and ineligible accounts, including storage or delivery failures.
	ResetRequest(w http.ResponseWriter, r *http.Request, tokenExpiration time.Duration)
	// GetReset renders the reset form after AuthManager validates token.
	GetReset(w http.ResponseWriter, r *http.Request, token string)
	// Reset must revalidate and consume the exact token atomically with the credential
	// change. AuthManager's earlier validation does not prevent concurrent reuse.
	// After commit, clear the request's cached identity before writing the success response.
	Reset(w http.ResponseWriter, r *http.Request, u *user.User, token string, requireNewCredentials bool)
}

// ChangeHandler supplies credential changes for an authenticated account.
type ChangeHandler interface {
	// GetChange renders the credential-change form for the authenticated account.
	GetChange(w http.ResponseWriter, r *http.Request)
	// Change checks existing credentials, applies their replacement, and writes the response.
	Change(w http.ResponseWriter, r *http.Request, u *user.User)
}

// Register adds a factory description without creating or initializing a provider.
// Descriptor errors are reported when an application creates its registry.
func Register(d Descriptor) {
	catalog.Lock()
	defer catalog.Unlock()
	catalog.descriptors = append(catalog.descriptors, d)
}

// Registered returns a copy of the import catalog.
func Registered() []Descriptor {
	catalog.RLock()
	defer catalog.RUnlock()
	return slices.Clone(catalog.descriptors)
}

// Loaded returns this application's providers whose IsLoaded method reports true.
func (r *Registry) Loaded() []AuthService {
	if r == nil {
		return nil
	}
	var loadedAuthServices []AuthService
	for _, s := range r.services {
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
