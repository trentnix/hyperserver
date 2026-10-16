package main

import (
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/messaging/smtp"
)

// configureMail selects local Mailpit unless the application supplies another
// sender. Port zero disables SMTP. Authentication validates required delivery
// before accepting requests. No connections remain open between sends.
func configureMail(s settings) (*messaging.MailClient, error) {
	sender := s.mailSender
	if sender == nil {
		if s.smtpPort == 0 {
			return nil, nil
		}
		var err error
		sender, err = smtp.NewSender(config.MailConfig{
			Hostname: "127.0.0.1", Port: s.smtpPort,
			User: "local", Password: "local", Timeout: 3 * time.Second,
		})
		if err != nil {
			return nil, err
		}
	}
	return messaging.NewMailClientWithSender("Notes <notes@example.invalid>", sender)
}
