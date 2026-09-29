package messaging

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	mailaddr "net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// ErrMailUnavailable means the sender is not configured to accept mail.
var ErrMailUnavailable = errors.New("mail delivery is unavailable")

const defaultMailTimeout = 30 * time.Second

type (
	// MailClient composes messages and passes them to a sender.
	MailClient struct {
		from   string
		sender MailSender
	}

	// MailMessage contains the addresses, subject, and HTML body of an email.
	// From and To each contain one mailbox, with an optional display name.
	// Body must contain trusted HTML with dynamic values already escaped.
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
		// Implementations must honor context cancellation. An error can occur
		// after acceptance, so callers must not assume retrying is safe.
		Send(context.Context, MailMessage) error
	}

	smtpSender struct {
		config      config.MailConfig
		dialContext func(context.Context, string, string) (net.Conn, error)
		rootCAs     *x509.CertPool // Nil uses system roots. Tests can trust a local peer.
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
	mailConfig := cfg.Mail
	if mailConfig.Timeout < 0 {
		return nil, errors.New("mail timeout must not be negative")
	}
	if mailConfig.Timeout == 0 {
		mailConfig.Timeout = defaultMailTimeout
	}
	return NewMailClientWithSender(mailConfig.FromAddress, &smtpSender{
		config: mailConfig, dialContext: (&net.Dialer{}).DialContext,
	})
}

// NewMailClientWithSender uses the supplied sender without an SMTP fallback.
// The caller must supply a configured sender. Senders can implement ValidateConfig()
// error for additional local checks. Validation must not contact external services.
func NewMailClientWithSender(from string, sender MailSender) (*MailClient, error) {
	if sender == nil {
		return nil, errors.New("mail sender is required")
	}
	return &MailClient{from: from, sender: sender}, nil
}

// ValidateConfig checks whether delivery is configured, not whether a remote
// service is reachable. Optional mail clients can remain unconfigured.
func (m *MailClient) ValidateConfig() error {
	if m == nil || m.sender == nil {
		return ErrMailUnavailable
	}
	if _, err := parseMailAddress("from", m.from); err != nil {
		return err
	}
	if validator, ok := m.sender.(interface{ ValidateConfig() error }); ok {
		return validator.ValidateConfig()
	}
	return nil
}

// Compose creates a new email.
func (m *MailClient) Compose() *mail {
	return &mail{
		client: m,
		from:   m.from,
	}
}

func (m *smtpSender) ValidateConfig() error {
	if m.config.Hostname == "" || m.config.Port == 0 || m.config.User == "" || m.config.Password == "" {
		return ErrMailUnavailable
	}
	return nil
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

	message := MailMessage{
		From: email.from, To: email.to, Subject: email.subject, Body: email.body,
	}
	if _, _, err := message.validateHeaders(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.sender.Send(ctx, message)
}

func safeHeader(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsFunc(value, func(r rune) bool {
		return r < 32 || r == 127
	})
}

func (m MailMessage) validateHeaders() (*mailaddr.Address, *mailaddr.Address, error) {
	if !safeHeader(m.Subject) {
		return nil, nil, errors.New("email subject contains invalid header characters")
	}
	from, err := parseMailAddress("from", m.From)
	if err != nil {
		return nil, nil, err
	}
	to, err := parseMailAddress("to", m.To)
	if err != nil {
		return nil, nil, err
	}
	return from, to, nil
}

func parseMailAddress(field, value string) (*mailaddr.Address, error) {
	if !safeHeader(value) {
		return nil, fmt.Errorf("email %s contains invalid header characters", field)
	}
	address, err := mailaddr.ParseAddress(value)
	if err != nil || !safeHeader(address.Name) || !safeHeader(address.Address) {
		return nil, fmt.Errorf("email %s must contain one valid address", field)
	}
	return address, nil
}

func smtpMailbox(address *mailaddr.Address) string {
	// Format without the display name to restore any required local-part quoting.
	// net/smtp adds its own angle brackets around the resulting mailbox.
	formatted := (&mailaddr.Address{Address: address.Address}).String()
	return strings.TrimSuffix(strings.TrimPrefix(formatted, "<"), ">")
}

func (m *smtpSender) Send(ctx context.Context, email MailMessage) (err error) {
	if err := m.ValidateConfig(); err != nil {
		return err
	}
	from, to, err := email.validateHeaders()
	if err != nil {
		return err
	}

	cfg := m.config
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	// Socket deadlines can fire just before the context timer. Report the same
	// context error in either case so callers can recognize interrupted sends.
	defer func() {
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			err = ctx.Err()
			return
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() && !time.Now().Before(deadline) {
			err = context.DeadlineExceeded
		}
	}()

	hostPort := net.JoinHostPort(cfg.Hostname, strconv.Itoa(int(cfg.Port)))
	conn, err := m.dialContext(ctx, "tcp", hostPort)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	tlsConfig := &tls.Config{ServerName: cfg.Hostname, MinVersion: tls.VersionTLS12, RootCAs: m.rootCAs}
	var smtpConn net.Conn = conn
	implicitTLS := cfg.Port == 465 || cfg.Port == 2465
	if implicitTLS {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return err
		}
		smtpConn = tlsConn
	}
	client, err := smtp.NewClient(smtpConn, cfg.Hostname)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return err
	}
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return err
			}
		}
	}
	if ok, _ := client.Extension("AUTH"); !ok {
		return errors.New("smtp: server doesn't support AUTH")
	}
	if err := client.Auth(smtp.PlainAuth("", cfg.User, cfg.Password, cfg.Hostname)); err != nil {
		return err
	}
	if err := client.Mail(smtpMailbox(from)); err != nil {
		return err
	}
	if err := client.Rcpt(smtpMailbox(to)); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}

	headers := []string{
		"From: " + from.String(),
		"To: " + to.String(),
		"Subject: " + mime.QEncoding.Encode("UTF-8", email.Subject),
		"MIME-Version: 1.0",
		`Content-Type: text/html; charset="UTF-8"`,
	}
	msg := strings.Join(headers, "\r\n") + "\r\n\r\n" + email.Body

	if _, err := writer.Write([]byte(msg)); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	// A later error does not prove the message was rejected. Do not retry here.
	if err := client.Quit(); err != nil {
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

// Body sets trusted HTML. Callers must escape dynamic values with html/template.
func (m *mail) Body(body string) *mail {
	m.body = body
	return m
}

// Send attempts to send the email.
func (m *mail) Send(ctx context.Context) error {
	return m.client.send(m, ctx)
}
