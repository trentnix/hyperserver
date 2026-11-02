package google

import (
	"context"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// Embedded methods panic if initialization accidentally accesses storage.
type setupAccounts struct {
	user.AccountRepository
	user.ExternalIdentityRepository
}
type setupStore struct {
	session.SessionStore
	session.RotatingStore
}

func TestProviderUsesApplicationDependencies(t *testing.T) {
	descriptor := Provider()
	accounts := &setupAccounts{}
	sessions := &session.SessionManager{}
	called := false
	dependencies := auth.Dependencies{Accounts: accounts, Sessions: sessions, Policy: auth.Policy{
		AllowAccount: func(context.Context, string) (bool, error) {
			called = true
			return true, nil
		},
	}}
	first := descriptor.New(dependencies).(*Service)
	second := descriptor.New(auth.Dependencies{}).(*Service)
	if first == second || first.accounts != accounts || first.sessions != sessions ||
		second.accounts != nil || second.sessions != nil || second.Policy.AllowAccount != nil {
		t.Fatal("provider did not isolate application dependencies")
	}
	first.Policy.AllowAccount(t.Context(), "person@example.com")
	if !called {
		t.Fatal("provider did not preserve application policy")
	}
	for _, registered := range auth.Registered() {
		if registered.Name == AuthType {
			got := registered.New(dependencies).(*Service)
			if got.accounts != accounts || got.sessions != sessions || got.Policy.AllowAccount == nil {
				t.Fatal("registered factory ignored application dependencies")
			}
			return
		}
	}
	t.Fatal("Google provider was not registered")
}

func TestGoogleInitialization(t *testing.T) {
	for _, name := range []string{"valid", "missing client", "missing secret", "missing origin", "insecure origin", "unknown option", "missing accounts", "missing identities", "missing sessions"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{HTTP: config.HTTPConfig{PublicOrigin: "https://example.com"}, Auth: config.AuthConfig{Services: map[string]map[string]string{"google": {"enabled": "true", "clientID": "client", "clientSecret": "secret"}}}}
			sessions, err := session.NewSessionManager(t.Context(), map[string]string{"default": "test"}, map[string]session.StoreFactory{"test": func(context.Context) (session.SessionStore, error) { return setupStore{}, nil }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sessions.Close() })
			provider := New(setupAccounts{}, sessions)
			switch name {
			case "missing client":
				cfg.Auth.Services["google"]["clientID"] = ""
			case "missing secret":
				cfg.Auth.Services["google"]["clientSecret"] = ""
			case "missing origin":
				cfg.HTTP.PublicOrigin = ""
			case "insecure origin":
				cfg.HTTP.PublicOrigin = "http://example.com"
			case "unknown option":
				cfg.Auth.Services["google"]["issuer"] = "https://attacker.invalid"
			case "missing accounts":
				provider.accounts = nil
			case "missing identities":
				provider.accounts = &struct{ user.AccountRepository }{}
			case "missing sessions":
				provider.sessions = nil
			}
			err = provider.Init(&server.ApplicationServer{Config: cfg, ContentManager: content.NewContentManager()})
			if (err == nil) != (name == "valid") {
				t.Fatalf("initialization: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "attacker.invalid") {
				t.Fatal("initialization disclosed configured value")
			}
		})
	}
}
