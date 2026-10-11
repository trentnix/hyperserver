package main

import (
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/messaging/smtp"
)

// initializeMail supplies the reference application's SMTP client unless a client
// was already supplied. It does not contact the server. Consumers that require
// delivery must validate configuration before requests are served.
func initializeMail(s *application) error {
	if s.Mail != nil {
		return nil
	}

	sender, err := smtp.NewSender(s.Config.Mail)
	if err != nil {
		return err
	}
	client, err := messaging.NewMailClientWithSender(s.Config.Mail.FromAddress, sender)
	if err != nil {
		return err
	}
	s.Mail = client
	return nil
}
