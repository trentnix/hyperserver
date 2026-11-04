package auth

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"reflect"
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
// New receives application-owned dependencies and must return a fresh instance
// without I/O. Providers must preserve and enforce the supplied Policy for
// registration and linking. Name must match AuthType.
// The instance must not be nil, including a typed nil inside an interface.
type Descriptor struct {
	Name string
	New  func(Dependencies) AuthService
}

// Registry holds one application's enabled providers. Initialize them before
// serving requests. A registry must not be shared between applications.
type Registry struct {
	services []AuthService
}

// NewRegistry validates provider selections and creates enabled instances only.
// The catalog can be an import snapshot or an application-supplied local catalog.
func NewRegistry(c *config.Config, descriptors []Descriptor, dependencies Dependencies) (*Registry, error) {
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
		service := d.New(dependencies)
		if isNilAuthService(service) || service.AuthType() != d.Name {
			return nil, fmt.Errorf("auth provider %q returned no instance or a different AuthType", d.Name)
		}
		r.services = append(r.services, service)
	}
	return r, nil
}

// isNilAuthService rejects typed nils before registry construction calls methods.
func isNilAuthService(service AuthService) bool {
	if service == nil {
		return true
	}
	switch value := reflect.ValueOf(service); value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
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

// AuthService identifies and configures an authentication provider. Providers
// implement the HTTP capabilities they support separately. Providers do not own
// service resources. The auth module does not call Close or Shutdown on them.
// Configuration selects providers. Any Init failure stops application startup.
type AuthService interface {
	// Init validates settings and prepares templates using supplied services.
	// It must not open connections, query storage, send mail, start background work,
	// or initialize or close supplied services. Initialize resource-owning
	// dependencies in application setup.
	// Template preparation is synchronous. Init has no cancellation context.
	// On failure, discard the provider instance. Supplied services remain usable.
	Init(*server.ApplicationServer) error
	// AuthType returns the provider's route and configuration identifier.
	AuthType() string
}

// ProviderRoutes optionally binds provider-specific routes after initialization.
// Use mutations for state-changing routes to apply the authentication rate limit.
// Registration errors stop startup before accepting traffic.
type ProviderRoutes interface {
	Routes(routes, mutations *routing.Routes) error
}

// AccountAction links to a provider-owned account management page.
// URL must be a local absolute path. Labels are escaped by the account template.
type AccountAction struct {
	Label string
	URL   string
}

// AccountActionsProvider offers account controls for the authenticated user.
// Return only actions the account can use. This method must not perform I/O or
// change the user. The target handlers must enforce their own authorization.
type AccountActionsProvider interface {
	AccountActions(*user.User) []AccountAction
}

// LoginHandler supplies a provider control and handles authentication.
// Providers do not own login pages. The manager composes them at /login.
type LoginHandler interface {
	AuthType() string
	// GetLoginButton returns reviewed provider markup, never unescaped input.
	GetLoginButton() template.HTML
	// Login returns an account or a validated form for the shared page. A zero
	// result means the provider wrote a response, such as an OAuth redirect.
	Login(http.ResponseWriter, *http.Request) LoginResult
}

// LoginResult contains either an authenticated account or escaped form markup.
// Providers must not include passwords in Form or write a response with either result.
type LoginResult struct {
	Account *user.User
	Form    template.HTML
}

// LoginFormHandler offers an inline credential form instead of a button.
// LoginForm renders only provider controls, without a page layout or HTTP response.
// Its alternative control must link to /login?provider=<AuthType>.
type LoginFormHandler interface {
	LoginForm(*http.Request) (template.HTML, error)
}

// LoginAvailability reports whether an account has credentials for this provider.
// It controls presentation only. Providers without this capability stay visible.
type LoginAvailability interface {
	LoginAvailable(context.Context, *user.User) (bool, error)
}

// LoginCallbackHandler completes an external login. A successful callback returns
// an account without writing a response. A rejected callback writes its response.
// The provider must validate browser-bound state before accepting credentials.
type LoginCallbackHandler interface {
	Callback(http.ResponseWriter, *http.Request) *user.User
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

// RegistrationFormHandler offers an inline form on the shared registration page.
// Other enabled providers remain available through RegistrationAlternatives.
type RegistrationFormHandler interface {
	GetRegisterForm(http.ResponseWriter, *http.Request)
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
