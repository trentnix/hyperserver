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

func TestProviderSuppliesMail(t *testing.T) {
	client, err := messaging.NewMailClientWithSender("sender@example.invalid", accountMailSender(func(context.Context, messaging.MailMessage) error {
		t.Fatal("provider setup attempted delivery")
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	descriptor := Provider(client)
	first, second := descriptor.New().(*EmailAuthService), descriptor.New().(*EmailAuthService)
	if descriptor.Name != AuthTypeEmail || first == second || first.mailClient != client || second.mailClient != client {
		t.Fatal("provider did not create independent instances with the supplied client")
	}
	other := Provider(nil).New().(*EmailAuthService)
	second.mailClient = nil
	if first.mailClient != client || other.mailClient != nil {
		t.Fatal("provider instances share mutable mail selection")
	}

	for _, registered := range auth_services.Registered() {
		if registered.Name == AuthTypeEmail {
			if registered.New().(*EmailAuthService).mailClient != nil {
				t.Fatal("application mail escaped into the import catalog")
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
			app.AccountRepository = &emailAccountRepository{}
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
			module := auth_services.Module([]auth_services.Descriptor{Provider(client)}).New()
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
