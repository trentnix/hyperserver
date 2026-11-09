package auth_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth"
	email "github.com/trentnix/hyperserver/auth/modules/email"
	"github.com/trentnix/hyperserver/auth/modules/google"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Nil embedded methods panic if initialization performs storage I/O.
type basicAccountStore struct{ user.AccountRepository }
type externalAccountStore struct {
	basicAccountStore
	user.ExternalIdentityRepository
}
type passwordAccountStore struct {
	externalAccountStore
	user.PasswordRepository
}
type recoveryAccountStore struct {
	passwordAccountStore
	user.PasswordResetRepository
}
type verifiedAccountStore struct {
	recoveryAccountStore
	user.VerificationRepository
}

type capabilityMail struct{}

func (capabilityMail) Send(context.Context, messaging.MailMessage) error {
	panic("initialization attempted mail delivery")
}

func TestConfiguredAccountCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name          string
		accounts      user.AccountRepository
		email, google bool
		noVerify      bool
		noRegister    bool
		disabled      bool
		wantError     string
	}{
		{name: "Google only", accounts: externalAccountStore{}, google: true},
		{name: "Google missing identities", accounts: basicAccountStore{}, google: true, wantError: "external identity account storage"},
		{name: "email missing password changes", accounts: externalAccountStore{}, email: true, wantError: "password changes"},
		{name: "email missing recovery", accounts: passwordAccountStore{}, email: true, wantError: "password reset"},
		{name: "email missing verification", accounts: recoveryAccountStore{}, email: true, wantError: "email verification"},
		{name: "missing verification without registration requirement", accounts: recoveryAccountStore{}, email: true, noVerify: true, wantError: "email verification"},
		{name: "missing verification with registration disabled", accounts: recoveryAccountStore{}, email: true, noRegister: true, wantError: "email verification"},
		{name: "email without registration verification", accounts: verifiedAccountStore{}, email: true, noVerify: true},
		{name: "email registration disabled", accounts: verifiedAccountStore{}, email: true, noRegister: true},
		{name: "email only", accounts: verifiedAccountStore{}, email: true},
		{name: "combined missing verification", accounts: recoveryAccountStore{}, email: true, google: true, wantError: "email verification"},
		{name: "combined", accounts: verifiedAccountStore{}, email: true, google: true},
		{name: "authentication disabled", email: true, google: true, disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := moduleConfig()
			cfg.HTTP.PublicOrigin = "https://example.test"
			cfg.Auth.Enabled = !tc.disabled
			cfg.Auth.RegistrationEnabled = !tc.noRegister
			cfg.Auth.RegisterRequiresVerification = !tc.noVerify
			cfg.Auth.Services = map[string]map[string]string{
				"email":  {"enabled": "false"},
				"google": {"enabled": "false", "clientID": "client", "clientSecret": "secret"},
			}
			if tc.email {
				cfg.Auth.Services["email"]["enabled"] = "true"
			}
			if tc.google {
				cfg.Auth.Services["google"]["enabled"] = "true"
			}
			app, err := server.NewApplicationServer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { app.Shutdown() })
			var mail *messaging.MailClient
			if tc.email && !tc.disabled {
				mail, err = messaging.NewMailClientWithSender("sender@example.test", capabilityMail{})
				if err != nil {
					t.Fatal(err)
				}
			}
			module := auth.Module(auth.Dependencies{
				Accounts: tc.accounts,
				Sessions: moduleSessions(t, map[string]string{"default": "custom"}),
				Mail:     mail,
			}, []auth.Descriptor{email.Provider(), google.Provider()}).New()
			err = module.Init(t.Context(), app)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("got %v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := module.Routes(routing.NewRoutes(app.Web)); err != nil {
				t.Fatal(err)
			}
			_, verifies := tc.accounts.(user.VerificationRepository)
			_, pattern := app.Web.Handler(httptest.NewRequest("GET", "/auth/verify", nil))
			if (pattern == "GET /auth/verify") != (cfg.Auth.Enabled && verifies) {
				t.Fatalf("verification route does not match storage capability: %q", pattern)
			}
		})
	}
}
