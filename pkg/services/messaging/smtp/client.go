package smtp

import (
	"context"
	"errors"
	"net"
	"strconv"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

// NewMailClient creates a mail client and a TCP connectivity probe
// from one configuration snapshot. Construction performs no network I/O.
// Incomplete settings remain allowed. Consumers that require delivery must call
// the client's ValidateConfig. The application decides whether to run the probe.
func NewMailClient(cfg config.MailConfig) (*messaging.MailClient, func(context.Context) error, error) {
	sender, err := NewSender(cfg)
	if err != nil {
		return nil, nil, err
	}
	client, err := messaging.NewMailClientWithSender(cfg.FromAddress, sender)
	if err != nil {
		return nil, nil, err
	}
	return client, sender.CheckConnectivity, nil
}

// CheckConnectivity opens and closes a TCP connection using the sender's settings.
// It does not check SMTP, TLS, authentication, or delivery. The caller's deadline
// and the configured mail timeout bound the connection attempt, whichever is sooner.
// Like Send, it is safe for concurrent use after sender configuration is complete.
func (m *Sender) CheckConnectivity(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.config.Hostname == "" || m.config.Port == 0 {
		return messaging.ErrMailUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.Timeout)
	defer cancel()

	address := net.JoinHostPort(m.config.Hostname, strconv.Itoa(int(m.config.Port)))
	conn, err := m.dialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return errors.Join(ctx.Err(), conn.Close())
}
