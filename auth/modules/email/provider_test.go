package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Unimplemented repository and session operations panic if setup attempts I/O.
type providerSetupAccounts struct {
	user.AccountRepository
	closes int
}

func (a *providerSetupAccounts) Close() error { a.closes++; return nil }

type providerSetupStore struct {
	session.SessionStore
	session.RotatingStore
	closes int
}

func (*providerSetupStore) IsEnabled() bool { return true }
func (s *providerSetupStore) Close() error  { s.closes++; return nil }

func TestProviderSetupLeavesServiceOwnershipWithApplication(t *testing.T) {
	for _, tc := range []struct {
		name, wantError string
	}{
		{"success", ""},
		{"template failure", "unable to initialize the email authorization service"},
		{"verification failure", "mail delivery is unavailable"},
		{"route failure", "conflicts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, err := server.NewApplicationServer(config.Config{Auth: config.AuthConfig{
				Enabled: true, RegistrationEnabled: true, RegisterRequiresVerification: true,
				JwtKey: "test-key000000000000000000000000", VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
				Services: map[string]map[string]string{"email": {"enabled": "true"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := app.Shutdown(); err != nil {
					t.Error(err)
				}
			})

			accounts := new(providerSetupAccounts)
			app.OnShutdown("accounts", func(context.Context) error { return accounts.Close() })
			store := new(providerSetupStore)
			sessions, err := session.NewSessionManager(t.Context(), map[string]string{"default": "test"}, map[string]session.StoreFactory{
				"test": func(context.Context) (session.SessionStore, error) { return store, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			app.OnShutdown("sessions", func(context.Context) error { return sessions.Close() })
			client, err := messaging.NewMailClientWithSender("sender@example.invalid", accountMailSender(func(context.Context, messaging.MailMessage) error {
				t.Fatal("provider setup attempted mail delivery")
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "verification failure" {
				client = nil
			}
			descriptor := auth_services.Descriptor{Name: AuthTypeEmail, New: func() auth_services.AuthService {
				provider := Provider(accounts, sessions, client).New().(*EmailAuthService)
				if tc.name == "template failure" {
					provider.TemplateFS = fstest.MapFS{}
				}
				return provider
			}}
			if tc.name == "route failure" {
				app.Web.HandleFunc("GET /auth/login", func(http.ResponseWriter, *http.Request) {})
			}

			err = handlers.Setup(t.Context(), app, nil, []handlers.Descriptor{
				auth_services.Module(accounts, sessions, []auth_services.Descriptor{descriptor}),
			}, nil)
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("setup error = %v, want %q", err, tc.wantError)
			}
			if accounts.closes != 0 || store.closes != 0 {
				t.Fatal("provider setup or rollback closed application-owned services")
			}

			for range 2 {
				if err := app.Shutdown(); err != nil {
					t.Fatal(err)
				}
			}
			if accounts.closes != 1 || store.closes != 1 {
				t.Fatalf("application cleanup: accounts=%d sessions=%d, want one each", accounts.closes, store.closes)
			}
		})
	}
}

var (
	_ auth_services.AuthService         = (*EmailAuthService)(nil)
	_ auth_services.LoginHandler        = (*EmailAuthService)(nil)
	_ auth_services.RegistrationHandler = (*EmailAuthService)(nil)
	_ auth_services.ResetHandler        = (*EmailAuthService)(nil)
	_ auth_services.ChangeHandler       = (*EmailAuthService)(nil)
)

func TestProviderSuppliesDependencies(t *testing.T) {
	client, err := messaging.NewMailClientWithSender("sender@example.invalid", accountMailSender(func(context.Context, messaging.MailMessage) error {
		t.Fatal("provider setup attempted delivery")
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	accounts := &emailAccountRepository{}
	sessions := emailSessions(t, map[string]string{"default": "memory"})
	descriptor := Provider(accounts, sessions, client)
	first, second := descriptor.New().(*EmailAuthService), descriptor.New().(*EmailAuthService)
	if descriptor.Name != AuthTypeEmail || first == second {
		t.Fatal("provider did not create independent instances")
	}
	if first.mailClient != client || second.mailClient != client || first.accounts != accounts || second.accounts != accounts {
		t.Fatal("provider did not retain its supplied dependencies")
	}
	if first.sessions != sessions || second.sessions != sessions {
		t.Fatal("provider did not retain the supplied session manager")
	}
	other := Provider(nil, nil, nil).New().(*EmailAuthService)
	second.mailClient = nil
	second.accounts = nil
	second.sessions = nil
	if first.mailClient != client || first.accounts != accounts || other.mailClient != nil || other.accounts != nil {
		t.Fatal("provider instances share mutable dependency selection")
	}

	if first.sessions != sessions || other.sessions != nil {
		t.Fatal("provider instances share session selection")
	}

	for _, registered := range auth_services.Registered() {
		if registered.Name == AuthTypeEmail {
			provider := registered.New().(*EmailAuthService)
			if provider.mailClient != nil || provider.accounts != nil || provider.sessions != nil {
				t.Fatal("application dependencies escaped into the import catalog")
			}
			return
		}
	}
	t.Fatal("email provider did not self-register")
}

func TestProviderValidatesSigningKey(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		wantError bool
	}{
		{"missing", "", true},
		{"short", strings.Repeat("s", 31), true},
		{"placeholder", "placeholder-key-that-is-long-enough", true},
		{"minimum", strings.Repeat("s", 32), false},
		{"longer", strings.Repeat("s", 33), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions := emailSessions(t, map[string]string{"default": "memory"})
			service := Provider(&emailAccountRepository{}, sessions, nil).New().(*EmailAuthService)
			// Bypass shared configuration validation to exercise the provider boundary.
			app := &server.ApplicationServer{ContentManager: content.NewContentManager(), Config: &config.Config{
				Auth: config.AuthConfig{JwtKey: tc.key, Services: map[string]map[string]string{"email": {"enabled": "true"}}},
			}}
			err := service.Init(app)
			if (err != nil) != tc.wantError {
				t.Fatalf("Init error = %v, want error = %v", err, tc.wantError)
			}
			if err == nil {
				return
			}
			if service.config != nil || service.views != nil {
				t.Fatal("invalid signing key must be rejected before configuring the provider")
			}
			foundField := false
			for cause := err; cause != nil; cause = errors.Unwrap(cause) {
				foundField = foundField || strings.Contains(cause.Error(), "auth.jwtKey")
				if tc.key != "" && strings.Contains(cause.Error(), tc.key) {
					t.Fatal("initialization error disclosed signing key")
				}
			}
			if !foundField {
				t.Fatal("initialization error did not identify auth.jwtKey")
			}
		})
	}
}

func TestProviderMailRequirements(t *testing.T) {
	for _, test := range []struct {
		name     string
		supplied bool
		required bool
	}{
		{"missing optional mail", false, false},
		{"missing required mail", false, true},
		{"supplied optional mail", true, false},
		{"supplied required mail", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, err := server.NewApplicationServer(config.Config{Auth: config.AuthConfig{
				Enabled: true, RegistrationEnabled: true, RegisterRequiresVerification: test.required,
				JwtKey: "test-key000000000000000000000000", VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
				Services: map[string]map[string]string{"email": {"enabled": "true"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			accounts := &emailAccountRepository{}
			sessions := emailSessions(t, map[string]string{"default": "memory", "auth-user-session": "identity"})
			var client *messaging.MailClient
			if test.supplied {
				client, err = messaging.NewMailClientWithSender("sender@example.invalid", accountMailSender(func(context.Context, messaging.MailMessage) error {
					t.Fatal("initialization attempted delivery")
					return nil
				}))
				if err != nil {
					t.Fatal(err)
				}
			}
			module := auth_services.Module(accounts, sessions, []auth_services.Descriptor{Provider(accounts, sessions, client)}).New()
			err = module.Init(t.Context(), app)
			if test.required && !test.supplied {
				if !errors.Is(err, messaging.ErrMailUnavailable) {
					t.Fatalf("initialization error = %v, want unavailable mail", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProviderRequiresSuppliedSessions(t *testing.T) {
	app := &server.ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{
		Services: map[string]map[string]string{"email": {"enabled": "true"}},
	}}}
	provider := Provider(&emailAccountRepository{}, nil, nil).New()
	err := provider.Init(app)
	if !errors.Is(err, session.ErrSessionManagerNotFound) {
		t.Fatalf("initialization error = %v, want missing session manager", err)
	}
}

func TestProviderRejectsUnknownOptionsBeforeDependencies(t *testing.T) {
	for _, option := range []string{"typo", "registrationenabled", "enabled "} {
		t.Run(option, func(t *testing.T) {
			app := &server.ApplicationServer{Config: &config.Config{Auth: config.AuthConfig{
				Services: map[string]map[string]string{"email": {"enabled": "true", option: "secret-option-value"}},
			}}}
			provider := Provider(nil, nil, nil).New()
			err := provider.Init(app)
			if !errors.Is(err, auth_services.ErrEmailAuthServiceInit) || !strings.Contains(err.Error(), "auth.services.email accepts only the enabled option") || strings.Contains(err.Error(), "secret-option-value") {
				t.Fatalf("expected safe option error, got %v", err)
			}
			if app.Config.Auth.Services["email"][option] != "secret-option-value" {
				t.Fatal("validation changed configuration")
			}
		})
	}
}
