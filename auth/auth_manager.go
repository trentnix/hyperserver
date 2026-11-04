// Package auth coordinates authentication providers and their HTTP handlers.
// Go package imports register factories. The auth module creates and initializes
// each application's manager and enabled providers before binding their routes.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	authmiddleware "github.com/trentnix/hyperserver/auth/middleware"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/ratelimit"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

const messageTemplate = "templates/html/pages/message.html"

// AuthManager routes authentication requests to registered providers.
// Init must run before Routes. When authentication is enabled, supply Services
// before Init and initialize its providers before serving requests.
type AuthManager struct {
	Enabled  bool
	Services *Registry
	Policy   Policy

	accounts       user.AccountRepository
	sessions       *session.SessionManager
	httpConfig     config.HTTPConfig
	rateLimit      *config.ModuleRateLimitConfig
	contentManager *content_services.ContentManagerService
	templates      map[string]*content.Templates

	resetTokenExpiration time.Duration

	resetRequiresNewCredentials bool
	registrationEnabled         bool
}

// NewAuthManager supplies accounts and sessions for authentication handlers.
// The caller owns both services and must prepare them before Init. When authentication
// is enabled, set Services before initialization. Construction performs no I/O.
func NewAuthManager(accounts user.AccountRepository, sessions *session.SessionManager) *AuthManager {
	return &AuthManager{accounts: accounts, sessions: sessions}
}

// Init validates dependencies and configures authentication handlers.
func (a *AuthManager) Init(_ context.Context, s *server.ApplicationServer) error {
	if s.Config.Auth.Enabled && a.Services == nil {
		return errors.New("authentication requires an application-owned provider registry")
	}
	if s.Config.Auth.Enabled {
		if a.accounts == nil {
			return errors.New("authentication requires an account repository")
		}
		store, err := a.sessions.Store("auth-user-session")
		if err != nil {
			return fmt.Errorf("authentication requires session revocation: %w", err)
		}
		if _, ok := store.(session.RotatingStore); !ok {
			return errors.New("authentication requires session revocation: selected store does not support revocable session rotation")
		}
		if _, err := a.sessions.Store(session.AuthSession); err != nil {
			return fmt.Errorf("authentication requires a redirect session: %w", err)
		}
	}

	a.Enabled = s.Config.Auth.Enabled
	a.httpConfig = s.Config.HTTP
	a.rateLimit = s.Config.Auth.RateLimit

	if s.ContentManager == nil {
		return errors.New("Content Manager not configured")
	}

	a.contentManager = s.ContentManager

	a.resetTokenExpiration = s.Config.Auth.ResetTokenExpiration

	a.resetRequiresNewCredentials = s.Config.Auth.ResetRequiresNewCredentials
	a.registrationEnabled = s.Config.Auth.Enabled && s.Config.Auth.RegistrationEnabled

	return a.prepareTemplates()
}

func (a *AuthManager) prepareTemplates() error {
	prepared := make(map[string]*content.Templates)
	for _, name := range []string{
		accountTemplate,
		authLoginSelectionTemplate,
		authRegisterSelectionTemplate,
		authVerifyTemplate,
		"templates/html/pages/verification-request.html",
	} {
		templates, err := content.PrepareTemplates(a.contentManager,
			content.TemplatePath{FS: authModuleTemplatesFS, Path: "templates/html/pages/provider.html"},
			content.TemplatePath{FS: authModuleTemplatesFS, Path: name})
		if err != nil {
			return fmt.Errorf("prepare auth template %s: %w", name, err)
		}
		prepared[name] = templates
	}
	a.templates = prepared
	return nil
}

// Routes registers auth-owned /auth operations. Site modules register the root
// login and registration pages using PageHandlers.
func (a *AuthManager) Routes(mux *routing.Routes) error {
	if a.Enabled {
		policy, err := a.mutationPolicy()
		if err != nil {
			return err
		}
		anonymous := func(handler http.Handler) http.Handler {
			return noStore(authmiddleware.RequireAnonymous(a.accounts, a.contentManager)(handler))
		}
		authenticated := func(handler http.Handler) http.Handler {
			return noStore(authmiddleware.RequireAuthentication(a.accounts, a.contentManager)(handler))
		}
		mutations, err := mux.WithRateLimit(policy)
		if err != nil {
			return err
		}

		// login / logout
		mux.Handle("GET /auth/login", noStore(http.HandlerFunc(a.redirectLogin)))
		mux.Handle("GET /auth/login/{authType}", noStore(http.HandlerFunc(a.GetLoginService)))
		for _, provider := range a.Services.Services() {
			if _, ok := provider.(LoginCallbackHandler); ok {
				mutations.Handle("GET /auth/callback/{authType}", noStore(http.HandlerFunc(a.Callback)))
				break
			}
		}
		mux.WithoutRateLimit().Handle("POST /auth/logout", noStore(http.HandlerFunc(a.Logout)))

		// register
		if a.registrationEnabled {
			mux.Handle("GET /auth/register", noStore(http.HandlerFunc(a.redirectRegistration)))
			mux.Handle("GET /auth/register/{authType}", noStore(http.HandlerFunc(a.GetRegisterService)))
		} else {
			mux.Handle("/auth/register", noStore(http.HandlerFunc(RegistrationUnavailable)))
			mux.Handle("/auth/register/{authType}", noStore(http.HandlerFunc(RegistrationUnavailable)))
		}

		// validate a registered user
		mux.Handle("GET /auth/verify", noStore(http.HandlerFunc(a.GetVerify)))
		mux.Handle("GET /auth/request/verify", noStore(http.HandlerFunc(a.GetVerificationRequest)))
		mutations.Handle("POST /auth/verify", noStore(http.HandlerFunc(a.Verify)))
		mutations.Handle("POST /auth/request/verify", noStore(http.HandlerFunc(a.SendVerificationRequest)))

		// reset credentials
		mux.Handle("GET /auth/reset/request/{authType}", anonymous(http.HandlerFunc(a.GetResetRequest)))
		mutations.Handle("POST /auth/reset/request/{authType}", anonymous(http.HandlerFunc(a.ResetRequest)))
		mux.Handle("GET /auth/reset/{authType}", anonymous(http.HandlerFunc(a.GetReset)))
		mutations.Handle("POST /auth/reset/{authType}", anonymous(http.HandlerFunc(a.Reset)))

		// account management
		mux.Handle("GET /auth/account", authenticated(http.HandlerFunc(a.GetAccount)))
		mux.Handle("GET /auth/change/{authType}", authenticated(http.HandlerFunc(a.GetChange)))
		mutations.Handle("POST /auth/change/{authType}", authenticated(http.HandlerFunc(a.Change)))

		for _, provider := range a.Services.Services() {
			if binder, ok := provider.(ProviderRoutes); ok {
				if err := binder.Routes(mux, mutations); err != nil {
					return fmt.Errorf("register auth service %q routes: %w", provider.AuthType(), err)
				}
				if err := mux.Err(); err != nil {
					return fmt.Errorf("register auth service %q routes: %w", provider.AuthType(), err)
				}
			}
		}
	}
	return mux.Err()
}

func (a *AuthManager) mutationPolicy() (ratelimit.Policy, error) {
	maxClients, err := a.rateLimit.ClientCapacity()
	if err != nil {
		return ratelimit.Policy{}, fmt.Errorf("auth.rateLimit.%w", err)
	}
	return ratelimit.Policy{Requests: 20, Window: time.Minute, MaxClients: maxClients}, nil
}

// noStore applies the auth response policy before handlers or access checks write
// a form, redirect, or error. Other modules choose their own cache policies.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// getAuthService finds a configured provider by its authType.
func (a *AuthManager) getAuthService(authType string) AuthService {
	authServices := a.Services.Services()
	for _, service := range authServices {
		if authType == service.AuthType() {
			return service
		}
	}

	return nil
}
