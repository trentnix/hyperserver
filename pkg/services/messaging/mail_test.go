package messaging

import (
	"context"
	"errors"
	"testing"

	"github.com/trentnix/hyperserver/config"
)

type senderFunc func(context.Context, MailMessage) error

func (f senderFunc) Send(ctx context.Context, message MailMessage) error {
	return f(ctx, message)
}

func TestMailClientUsesInjectedSender(t *testing.T) {
	for _, from := range []string{"", "override@example.invalid"} {
		t.Run("from="+from, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := errors.New("delivery failed")
			calls := 0
			client, err := NewMailClientWithSender("default@example.invalid", senderFunc(func(gotCtx context.Context, message MailMessage) error {
				calls++
				wantFrom := from
				if wantFrom == "" {
					wantFrom = "default@example.invalid"
				}
				want := MailMessage{From: wantFrom, To: "recipient@example.invalid", Subject: "Test", Body: "<p>Hello</p>"}
				if message != want || gotCtx != ctx {
					t.Errorf("sender received message %+v and context %v, want %+v and original context", message, gotCtx, want)
				}
				return wantErr
			}))
			if err != nil {
				t.Fatal(err)
			}
			message := client.Compose().To("recipient@example.invalid").Subject("Test").Body("<p>Hello</p>")
			if from != "" {
				message.From(from)
			}
			if err := message.Send(ctx); !errors.Is(err, wantErr) {
				t.Fatalf("Send error = %v, want %v", err, wantErr)
			}
			if calls != 1 {
				t.Fatalf("sender calls = %d, want 1", calls)
			}
		})
	}
}

func TestMailClientRejectsIncompleteMessages(t *testing.T) {
	for _, tc := range []struct {
		name, from, to, body, want string
	}{
		{"from", "", "to@example.invalid", "Hello", "email cannot be sent without a from address"},
		{"to", "from@example.invalid", "", "Hello", "email cannot be sent without a to address"},
		{"body", "from@example.invalid", "to@example.invalid", "", "email cannot be sent without a body to render"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewMailClientWithSender(tc.from, senderFunc(func(context.Context, MailMessage) error {
				t.Fatal("sender called for an incomplete message")
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Compose().To(tc.to).Body(tc.body).Send(context.Background()); err == nil || err.Error() != tc.want {
				t.Fatalf("Send error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMailClientRequiresDependencies(t *testing.T) {
	if _, err := NewMailClient(nil); err == nil {
		t.Fatal("accepted nil configuration")
	}
	if _, err := NewMailClientWithSender("from@example.invalid", nil); err == nil {
		t.Fatal("accepted nil sender")
	}
}

func TestSMTPUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		omit func(*config.MailConfig)
	}{
		{"all settings", func(c *config.MailConfig) { *c = config.MailConfig{FromAddress: c.FromAddress} }},
		{"hostname", func(c *config.MailConfig) { c.Hostname = "" }},
		{"port", func(c *config.MailConfig) { c.Port = 0 }},
		{"user", func(c *config.MailConfig) { c.User = "" }},
		{"password", func(c *config.MailConfig) { c.Password = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Mail: config.MailConfig{
				Hostname: "smtp.example.invalid", Port: 587,
				User: "test-user", Password: "test-password", FromAddress: "from@example.invalid",
			}}
			tc.omit(&cfg.Mail)
			client, err := NewMailClient(cfg)
			if err != nil {
				t.Fatalf("unconfigured mail must not prevent client construction: %v", err)
			}
			err = client.Compose().To("to@example.invalid").Subject("Test").Body("Hello").Send(context.Background())
			if !errors.Is(err, ErrMailUnavailable) {
				t.Fatalf("Send error = %v, want ErrMailUnavailable", err)
			}
		})
	}
}

func TestMailClientReportsSenderOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"accepted", nil},
		{"unavailable", ErrMailUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
				calls++
				return tc.err
			}))
			if err != nil {
				t.Fatal(err)
			}
			err = client.Compose().To("to@example.invalid").Body("Hello").Send(context.Background())
			if !errors.Is(err, tc.err) || calls != 1 {
				t.Fatalf("Send = %v, calls = %d, want %v and one call", err, calls, tc.err)
			}
		})
	}
}
