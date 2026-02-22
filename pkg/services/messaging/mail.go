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

type (
	// MailClient provides a client for sending email
	// This is purposely not completed because there are many different methods and services
	// for sending email, many of which are very different. Choose what works best for you
	// and populate the methods below. For now, emails will just be logged.
	MailClient struct {
		// config stores application configuration.
		config *config.Config
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

// NewMailClient creates a new MailClient.
func NewMailClient(cfg *config.Config) (*MailClient, error) {
	return &MailClient{
		config: cfg,
	}, nil
}

// Compose creates a new email.
func (m *MailClient) Compose() *mail {
	return &mail{
		client: m,
		from:   m.config.Mail.FromAddress,
	}
}

// skipSend determines if mail sending should be skipped.
func (m *MailClient) skipSend() bool {
	return m.config == nil ||
		m.config.Mail.Hostname == "" ||
		m.config.Mail.Port == 0 ||
		m.config.Mail.User == "" ||
		m.config.Mail.Password == ""
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

	// Check if mail sending should be skipped.
	if m.skipSend() {
		if ctxLogger := logger.Get(ctx); ctxLogger != nil {
			(*ctxLogger).Info("Skipped sending email", logger.Field{Key: "to", Value: email.to})
		} else {
			log.Printf("Skipped sending email to=%s", email.to)
		}
		return nil
	}

	cfg := m.config.Mail
	hostPort := net.JoinHostPort(cfg.Hostname, strconv.Itoa(int(cfg.Port)))
	auth := smtp.PlainAuth("", cfg.User, cfg.Password, cfg.Hostname)

	headers := []string{
		fmt.Sprintf("From: %s", email.from),
		fmt.Sprintf("To: %s", email.to),
		fmt.Sprintf("Subject: %s", email.subject),
		"MIME-Version: 1.0",
		`Content-Type: text/html; charset="UTF-8"`,
	}
	msg := strings.Join(headers, "\r\n") + "\r\n\r\n" + email.body

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
		if mailErr := client.Mail(email.from); mailErr != nil {
			return mailErr
		}
		if rcptErr := client.Rcpt(email.to); rcptErr != nil {
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
		err = smtp.SendMail(hostPort, auth, email.from, []string{email.to}, []byte(msg))
	}

	if err != nil {
		return err
	}

	if ctxLogger := logger.Get(ctx); ctxLogger != nil {
		(*ctxLogger).Info("Sent email", logger.Field{Key: "to", Value: email.to})
	} else {
		log.Printf("Sent email to=%s", email.to)
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
