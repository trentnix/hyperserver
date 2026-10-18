package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

func TestMailInitializesExplicitly(t *testing.T) {
	cfg := config.Config{Database: config.DatabaseConfig{Driver: "unknown"}, Mail: config.MailConfig{Timeout: -time.Second}}
	cfg.HTTP.Session.Types = map[string]string{"default": "unknown"}
	app, err := newApplication(cfg)
	if err != nil {
		t.Fatal("unused services prevented construction:", err)
	}
	if err := initializeMail(app); err == nil || app.Mail != nil || app.mailHealth != nil {
		t.Fatal("invalid mail initialization succeeded")
	}
	app.Config.Mail.Timeout = 0
	app.Config.Mail.FromAddress = "test@example.invalid"
	if err := initializeMail(app); err != nil {
		t.Fatal("mail retry:", err)
	}
	if app.mailHealth == nil {
		t.Fatal("SMTP setup omitted its connectivity probe")
	}
	mail := app.Mail
	app.Config.Mail.Timeout = -time.Second
	if err := initializeMail(app); err != nil || app.Mail != mail {
		t.Fatal("repeated initialization replaced the client:", err)
	}
	if !errors.Is(mail.ValidateConfig(), messaging.ErrMailUnavailable) {
		t.Fatal("unconfigured mail reported available delivery")
	}
	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

func TestMailSetupPreservesReplacement(t *testing.T) {
	app, err := newApplication(config.Config{Mail: config.MailConfig{Timeout: -time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeMailSender{}
	client, err := messaging.NewMailClientWithSender("replacement@example.invalid", sender)
	if err != nil {
		t.Fatal(err)
	}
	app.Mail = client
	probes := 0
	app.mailHealth = func(context.Context) error { probes++; return nil }
	if err := initializeMail(app); err != nil || app.Mail != client {
		t.Fatal("setup replaced the supplied client or used SMTP settings:", err)
	}
	if err := app.Mail.ValidateConfig(); err != nil {
		t.Fatal(err)
	}
	if len(sender.snapshot()) != 0 {
		t.Fatal("setup attempted mail delivery")
	}
	if probes != 0 {
		t.Fatal("setup ran the supplied readiness probe")
	}
	if err := app.mailHealth(t.Context()); err != nil || probes != 1 {
		t.Fatal("setup replaced the supplied readiness probe:", err)
	}

	if err := app.Mail.Compose().To("recipient@example.invalid").Body("Hello").Send(context.Background()); err != nil {
		t.Fatal(err)
	}
	sent := sender.snapshot()
	if len(sent) != 1 || sent[0].From != "replacement@example.invalid" || sent[0].To != "recipient@example.invalid" {
		t.Fatalf("replacement delivery: %+v", sent)
	}
}
