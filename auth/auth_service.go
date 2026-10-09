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
