package smtp

import (
	"context"
	"net"
	"testing"
	"time"

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
			client, err := newTestMailClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			client.sender.dialContext = func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("configuration validation contacted the SMTP server")
				return nil, nil
			}
			if err := client.ValidateConfig(); (err != nil) != tc.wantError {
				t.Fatalf("ValidateConfig error = %v, want error = %t", err, tc.wantError)
			}
		})
	}
}

func TestSenderOwnsConfiguration(t *testing.T) {
	cfg := config.MailConfig{Hostname: "smtp.example.invalid", Port: 587, User: "test", Password: "test"}
	sender, err := NewSender(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != 0 {
		t.Fatal("constructor mutated source settings")
	}

	cfg.Hostname = ""
	cfg.Timeout = -1
	if err := sender.ValidateConfig(); err != nil {
		t.Fatal("source mutation changed the sender:", err)
	}
	if sender.config.Timeout != 30*time.Second {
		t.Fatal("sender lost its default timeout")
	}
}
