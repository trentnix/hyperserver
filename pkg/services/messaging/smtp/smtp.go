// Package smtp supplies a context-aware SMTP mail sender.
package smtp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/quotedprintable"
	"net"
	mailaddr "net/mail"
	netsmtp "net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/idna"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

// Sender submits messages over SMTP. Construct it with NewSender and finish
// configuration before sharing it across requests. Send is safe for concurrent use.
type Sender struct {
	config      config.MailConfig
	dialContext func(context.Context, string, string) (net.Conn, error)
	rootCAs     *x509.CertPool // Nil uses system roots. Tests can trust a local peer.
}

// NewSender copies the supplied SMTP settings without contacting the server.
// A zero timeout uses 30 seconds. Negative timeouts return an error.
// Incomplete settings are allowed, but ValidateConfig and Send report unavailable delivery.
func NewSender(cfg config.MailConfig) (*Sender, error) {
	if cfg.Timeout < 0 {
		return nil, errors.New("mail timeout must not be negative")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Sender{config: cfg, dialContext: (&net.Dialer{}).DialContext}, nil
}

// ValidateConfig checks required SMTP settings without opening a connection.
func (m *Sender) ValidateConfig() error {
	if m.config.Hostname == "" || m.config.Port == 0 || m.config.User == "" || m.config.Password == "" {
		return messaging.ErrMailUnavailable
	}
	return nil
}

func smtpMailbox(address *mailaddr.Address) string {
	// Format without the display name to restore any required local-part quoting.
	// net/smtp adds its own angle brackets around the resulting mailbox.
	formatted := (&mailaddr.Address{Address: address.Address}).String()
	return strings.TrimSuffix(strings.TrimPrefix(formatted, "<"), ">")
}

// smtpAddress converts only the transport domain. Keep the local part unchanged,
// including its case and Unicode normalization, regardless of account lookup rules.
func smtpAddress(address *mailaddr.Address) (*mailaddr.Address, error) {
	separator := strings.LastIndexByte(address.Address, '@')
	domain := address.Address[separator+1:]
	if !containsNonASCII(domain) {
		return address, nil
	}
	profile := idna.New(idna.MapForLookup(), idna.Transitional(false),
		idna.BidiRule(), idna.VerifyDNSLength(true), idna.CheckHyphens(false))
	domain, err := profile.ToASCII(domain)
	if err != nil {
		return nil, errors.New("smtp: invalid internationalized address domain")
	}
	return &mailaddr.Address{Name: address.Name, Address: address.Address[:separator+1] + domain}, nil
}

func containsNonASCII(value string) bool {
	return strings.ContainsFunc(value, func(r rune) bool { return r >= utf8.RuneSelf })
}

// Send submits one message over SMTP within the configured timeout.
// It closes the connection when ctx is canceled. Internationalized local parts
// require SMTPUTF8 and 8BITMIME support. Domains use IDNA ASCII form for delivery.
func (m *Sender) Send(ctx context.Context, email messaging.MailMessage) (err error) {
	if err := m.ValidateConfig(); err != nil {
		return err
	}
	from, to, err := email.ValidateHeaders()
	if err != nil {
		return err
	}
	from, err = smtpAddress(from)
	if err != nil {
		return fmt.Errorf("email from: %w", err)
	}
	to, err = smtpAddress(to)
	if err != nil {
		return fmt.Errorf("email to: %w", err)
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

	client, err := netsmtp.NewClient(smtpConn, cfg.Hostname)
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

	if containsNonASCII(from.Address) || containsNonASCII(to.Address) {
		for _, extension := range []string{"SMTPUTF8", "8BITMIME"} {
			if ok, _ := client.Extension(extension); !ok {
				return fmt.Errorf("smtp: internationalized addresses require %s support", extension)
			}
		}
	}
	if ok, _ := client.Extension("AUTH"); !ok {
		return errors.New("smtp: server doesn't support AUTH")
	}
	if err := client.Auth(netsmtp.PlainAuth("", cfg.User, cfg.Password, cfg.Hostname)); err != nil {
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
		"Content-Transfer-Encoding: quoted-printable",
	}
	if _, err := io.WriteString(writer, strings.Join(headers, "\r\n")+"\r\n\r\n"); err != nil {
		return err
	}
	encoded := quotedprintable.NewWriter(writer)
	if _, err := io.WriteString(encoded, email.Body); err != nil {
		return err
	}
	if err := encoded.Close(); err != nil {
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
		ctxLogger.Info("Sent email", logger.Field{Key: "to", Value: email.To})
	} else {
		log.Printf("Sent email to=%s", email.To)
	}

	return nil
}
