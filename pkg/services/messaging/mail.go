package messaging

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// ErrMailUnavailable means the sender is not configured to accept mail.
var ErrMailUnavailable = errors.New("mail delivery is unavailable")

type (
	// MailClient composes messages and passes them to a sender.
	MailClient struct {
		from   string
		sender MailSender
	}

	// MailMessage contains the addresses, subject, and HTML body of an email.
	MailMessage struct {
		From    string
		To      string
		Subject string
		Body    string
	}

	// MailSender submits messages through SMTP or another delivery service.
	MailSender interface {
		// Send returns nil only when the service accepts the message for delivery.
		// Disabled or unavailable delivery must return an error. Acceptance does
		// not guarantee delivery to the recipient's inbox.
		Send(context.Context, MailMessage) error
	}

	smtpSender struct {
		config config.MailConfig
	}

	// mail represents an email to be sent.
	mail struct {
		client  *MailClient
		from    string
		to      string
		subject string
		body    string
	}
)

// NewMailClient creates an SMTP client. Incomplete SMTP settings do not prevent
// construction, but sending a valid message returns ErrMailUnavailable.
func NewMailClient(cfg *config.Config) (*MailClient, error) {
	if cfg == nil {
		return nil, errors.New("mail configuration is required")
	}
	return NewMailClientWithSender(cfg.Mail.FromAddress, &smtpSender{config: cfg.Mail})
}

// NewMailClientWithSender uses the supplied sender without an SMTP fallback.
func NewMailClientWithSender(from string, sender MailSender) (*MailClient, error) {
	if sender == nil {
		return nil, errors.New("mail sender is required")
	}
	return &MailClient{from: from, sender: sender}, nil
}

// Compose creates a new email.
func (m *MailClient) Compose() *mail {
	return &mail{
		client: m,
		from:   m.from,
	}
}

func (m *smtpSender) configured() bool {
	return m.config.Hostname != "" &&
		m.config.Port != 0 &&
		m.config.User != "" &&
		m.config.Password != ""
}

// send attempts to send the email.
func (m *MailClient) send(email *mail, ctx context.Context) error {
	switch {
	case email.from == "":
		return errors.New("email cannot be sent without a from address")
	case email.to == "":
		return errors.New("email cannot be sent without a to address")
	case email.body == "":
		return errors.New("email cannot be sent without a body to render")
	}

	return m.sender.Send(ctx, MailMessage{
		From: email.from, To: email.to, Subject: email.subject, Body: email.body,
	})
}

func (m *smtpSender) Send(ctx context.Context, email MailMessage) error {
	if !m.configured() {
		return ErrMailUnavailable
	}

	cfg := m.config
	hostPort := net.JoinHostPort(cfg.Hostname, strconv.Itoa(int(cfg.Port)))
	auth := smtp.PlainAuth("", cfg.User, cfg.Password, cfg.Hostname)

	headers := []string{
		fmt.Sprintf("From: %s", email.From),
		fmt.Sprintf("To: %s", email.To),
		fmt.Sprintf("Subject: %s", email.Subject),
		"MIME-Version: 1.0",
		`Content-Type: text/html; charset="UTF-8"`,
	}
	msg := strings.Join(headers, "\r\n") + "\r\n\r\n" + email.Body

	var err error
	switch cfg.Port {
	case 465, 2465:
		tlsConn, dialErr := tls.Dial("tcp", hostPort, &tls.Config{
			ServerName: cfg.Hostname,
			MinVersion: tls.VersionTLS12,
		})
		if dialErr != nil {
			return dialErr
		}
		defer tlsConn.Close()

		client, clientErr := smtp.NewClient(tlsConn, cfg.Hostname)
		if clientErr != nil {
			return clientErr
		}
		defer client.Close()

		if authErr := client.Auth(auth); authErr != nil {
			return authErr
		}
		if mailErr := client.Mail(email.From); mailErr != nil {
			return mailErr
		}
		if rcptErr := client.Rcpt(email.To); rcptErr != nil {
			return rcptErr
		}

		writer, dataErr := client.Data()
		if dataErr != nil {
			return dataErr
		}
		if _, writeErr := writer.Write([]byte(msg)); writeErr != nil {
			_ = writer.Close()
			return writeErr
		}
		if closeErr := writer.Close(); closeErr != nil {
			return closeErr
		}
		err = client.Quit()
	default:
		err = smtp.SendMail(hostPort, auth, email.From, []string{email.To}, []byte(msg))
	}

	if err != nil {
		return err
	}

	if ctxLogger := logger.Get(ctx); ctxLogger != nil {
		(*ctxLogger).Info("Sent email", logger.Field{Key: "to", Value: email.To})
	} else {
		log.Printf("Sent email to=%s", email.To)
	}

	return nil
}

// From sets the email from address.
func (m *mail) From(from string) *mail {
	m.from = from
	return m
}

// To sets the email address this email will be sent to.
func (m *mail) To(to string) *mail {
	m.to = to
	return m
}

// Subject sets the subject line of the email.
func (m *mail) Subject(subject string) *mail {
	m.subject = subject
	return m
}

// Body sets the body of the email.
func (m *mail) Body(body string) *mail {
	m.body = body
	return m
}

// Send attempts to send the email.
func (m *mail) Send(ctx context.Context) error {
	return m.client.send(m, ctx)
}
