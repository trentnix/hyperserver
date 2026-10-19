// Package messaging composes HTML email and sends it through a replaceable MailSender.
// Successful Send means the service accepted the message, not that it reached the inbox.
package messaging

import (
	"context"
	"errors"
	"fmt"
	mailaddr "net/mail"
	"strings"
	"unicode/utf8"
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
	// From and To each contain one mailbox, with an optional display name.
	// Body must contain trusted HTML with dynamic values already escaped.
	MailMessage struct {
		From    string
		To      string
		Subject string
		Body    string
	}

	// MailSender submits messages to a delivery service.
	MailSender interface {
		// Send returns nil only when the service accepts the message for delivery.
		// Disabled or unavailable delivery must return an error. Acceptance does
		// not guarantee delivery to the recipient's inbox.
		// Implementations must honor context cancellation. An error can occur
		// after acceptance, so callers must not assume retrying is safe.
		Send(context.Context, MailMessage) error
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

// NewMailClientWithSender uses the supplied sender without selecting a fallback.
// Callers must supply a valid sender, not a typed nil inside an interface.
// Senders can implement ValidateConfig() error for local availability checks.
// Validation must not contact external services.
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

// CheckHealth delegates to senders that implement CheckHealth(context.Context) error.
// Without that method, it only checks cancellation and client availability.
// Probes must be read-only, safe for concurrent calls, and honor ctx.
// A successful probe does not establish that mail can be delivered.
func (m *MailClient) CheckHealth(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil || m.sender == nil {
		return ErrMailUnavailable
	}
	if check, ok := m.sender.(interface{ CheckHealth(context.Context) error }); ok {
		return check.CheckHealth(ctx)
	}
	return nil
}

// Compose starts a message using the configured sender address.
// Set To, Subject, and Body on the result, then call Send(ctx). Body must contain
// trusted HTML with dynamic values escaped by the caller.
func (m *MailClient) Compose() *mail {
	return &mail{
		client: m,
		from:   m.from,
	}
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
	if _, _, err := message.ValidateHeaders(); err != nil {
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

// ValidateHeaders rejects unsafe header values and returns the parsed sender and
// recipient addresses. Mail providers must validate headers before delivery.
func (m MailMessage) ValidateHeaders() (*mailaddr.Address, *mailaddr.Address, error) {
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

// Send validates the message and submits it through the configured MailSender.
// A nil error means accepted for delivery, not confirmed delivery to the inbox.
func (m *mail) Send(ctx context.Context) error {
	return m.client.send(m, ctx)
}
