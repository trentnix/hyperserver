package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	auth_services "github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
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
	descriptor := Provider(accounts, client)
	first, second := descriptor.New().(*EmailAuthService), descriptor.New().(*EmailAuthService)
	if descriptor.Name != AuthTypeEmail || first == second {
		t.Fatal("provider did not create independent instances")
	}
	if first.mailClient != client || second.mailClient != client || first.accounts != accounts || second.accounts != accounts {
		t.Fatal("provider did not retain its supplied dependencies")
	}
	other := Provider(nil, nil).New().(*EmailAuthService)
	second.mailClient = nil
	second.accounts = nil
	if first.mailClient != client || first.accounts != accounts || other.mailClient != nil || other.accounts != nil {
		t.Fatal("provider instances share mutable dependency selection")
	}

	for _, registered := range auth_services.Registered() {
		if registered.Name == AuthTypeEmail {
			provider := registered.New().(*EmailAuthService)
			if provider.mailClient != nil || provider.accounts != nil {
				t.Fatal("application dependencies escaped into the import catalog")
			}
			return
		}
	}
	t.Fatal("email provider did not self-register")
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
				JwtKey: "test-key", VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour,
				Services: map[string]map[string]string{"email": {"enabled": "true"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			accounts := &emailAccountRepository{}
			app.SessionManager = emailSessions(t, map[string]string{"default": "memory", "auth-user-session": "identity"})
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
			module := auth_services.Module(accounts, []auth_services.Descriptor{Provider(accounts, client)}).New()
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
