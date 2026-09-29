package messaging

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/trentnix/hyperserver/config"
)

func TestMailConfigurationValidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*config.MailConfig)
		wantError bool
	}{
		{"configured", func(c *config.MailConfig) {}, false},
		{"missing host", func(c *config.MailConfig) { c.Hostname = "" }, true},
		{"missing port", func(c *config.MailConfig) { c.Port = 0 }, true},
		{"missing user", func(c *config.MailConfig) { c.User = "" }, true},
		{"missing password", func(c *config.MailConfig) { c.Password = "" }, true},
		{"missing sender", func(c *config.MailConfig) { c.FromAddress = "" }, true},
		{"invalid sender", func(c *config.MailConfig) { c.FromAddress = "not-an-address" }, true},
		{"injected sender", func(c *config.MailConfig) { c.FromAddress = "from@example.invalid\r\nBcc: other@example.invalid" }, true},
		{"encoded injection", func(c *config.MailConfig) { c.FromAddress = "=?UTF-8?Q?name=0D=0ABcc=3A?= <a@example.invalid>" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Mail: config.MailConfig{
				Hostname: "smtp.example.invalid", Port: 587, User: "test", Password: "test", FromAddress: "Test <from@example.invalid>",
			}}
			tc.change(&cfg.Mail)
			client, err := NewMailClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			client.sender.(*smtpSender).dialContext = func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("configuration validation contacted the SMTP server")
				return nil, nil
			}
			if err := client.ValidateConfig(); (err != nil) != tc.wantError {
				t.Fatalf("ValidateConfig error = %v, want error = %t", err, tc.wantError)
			}
		})
	}
}

type validatingSender struct {
	MailSender
	err   error
	calls int
}

func (s *validatingSender) ValidateConfig() error {
	s.calls++
	return s.err
}

func TestMailValidationUsesConfiguredSender(t *testing.T) {
	var absent *MailClient
	if err := absent.ValidateConfig(); !errors.Is(err, ErrMailUnavailable) {
		t.Fatalf("nil client error = %v", err)
	}
	if err := (&MailClient{}).ValidateConfig(); !errors.Is(err, ErrMailUnavailable) {
		t.Fatalf("missing sender error = %v", err)
	}
	client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
		t.Fatal("validation attempted delivery")
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ValidateConfig(); err != nil {
		t.Fatalf("configured replacement sender required SMTP settings: %v", err)
	}
	for _, validationErr := range []error{nil, ErrMailUnavailable} {
		sender := &validatingSender{err: validationErr}
		client, err := NewMailClientWithSender("from@example.invalid", sender)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.ValidateConfig(); !errors.Is(err, validationErr) || sender.calls != 1 {
			t.Fatalf("validation error = %v, calls = %d", err, sender.calls)
		}
	}
}
