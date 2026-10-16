package main

import (
	"context"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/messaging/smtp"
	"net"
	"strconv"
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
	// Probe connectivity only. Delivery failures still surface in the mail operation.
	s.mailHealth = func(ctx context.Context) error {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(s.Config.Mail.Hostname, strconv.Itoa(int(s.Config.Mail.Port))))
		if err != nil {
			return err
		}
		return conn.Close()
	}
	return nil
}
