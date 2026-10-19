package smtp

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
)

func TestNewMailClientConfiguration(t *testing.T) {
	client, err := NewMailClient(config.MailConfig{})
	if err != nil || client == nil {
		t.Fatalf("optional mail construction: client=%v error=%v", client, err)
	}
	if client.ValidateConfig() == nil || !errors.Is(client.CheckHealth(t.Context()), messaging.ErrMailUnavailable) {
		t.Fatal("unconfigured mail reported available")
	}
	client, err = NewMailClient(config.MailConfig{Timeout: -time.Second})
	if err == nil || client != nil {
		t.Fatal("failed construction returned usable values")
	}
}

func TestNewMailClientConnectivitySnapshot(t *testing.T) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	cfg := config.MailConfig{Hostname: "127.0.0.1", Port: uint16(listener.Addr().(*net.TCPAddr).Port),
		FromAddress: "sender@example.invalid", User: "user", Password: "password", Timeout: time.Second}
	client, err := NewMailClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Hostname, cfg.FromAddress, cfg.Port = "invalid.invalid", "invalid", 0
	if err := client.ValidateConfig(); err != nil {
		t.Fatal("client retained mutable configuration:", err)
	}
	if err := client.CheckHealth(t.Context()); err != nil {
		t.Fatal("probe did not use the configured snapshot:", err)
	}
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var data [1]byte
	if n, err := conn.Read(data[:]); n != 0 || err != io.EOF {
		t.Fatalf("probe wrote data or left its connection open: bytes=%d error=%v", n, err)
	}
}

type probeConnection struct {
	net.Conn // Any I/O other than Close is a test failure.
	close    func() error
}

func (c probeConnection) Close() error { return c.close() }

func TestCheckHealthFailures(t *testing.T) {
	for _, mode := range []string{"dial", "close", "canceled before dial", "canceled after dial", "missing host", "missing port"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.MailConfig{Hostname: "::1", Port: 2525}
			if mode == "missing host" {
				cfg.Hostname = ""
			}
			if mode == "missing port" {
				cfg.Port = 0
			}
			sender, err := NewSender(cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "canceled before dial" {
				cancel()
			}
			cause := errors.New("connection failed")
			dials, closes := 0, 0
			sender.dialContext = func(_ context.Context, network, address string) (net.Conn, error) {
				dials++
				if network != "tcp" || address != "[::1]:2525" {
					t.Fatalf("unexpected destination: %s %s", network, address)
				}
				if mode == "dial" {
					return nil, cause
				}
				if mode == "canceled after dial" {
					cancel()
				}
				return probeConnection{close: func() error { closes++; return cause }}, nil
			}
			err = sender.CheckHealth(ctx)
			want, wantDials, wantCloses := cause, 1, 1
			switch mode {
			case "dial":
				wantCloses = 0
			case "canceled before dial":
				want, wantDials, wantCloses = context.Canceled, 0, 0
			case "missing host", "missing port":
				want, wantDials, wantCloses = messaging.ErrMailUnavailable, 0, 0
			case "canceled after dial":
				if !errors.Is(err, context.Canceled) {
					t.Fatal("lost cancellation:", err)
				}
			}
			if !errors.Is(err, want) || dials != wantDials || closes != wantCloses {
				t.Fatalf("error=%v dials=%d closes=%d", err, dials, closes)
			}
		})
	}
}

func TestCheckHealthDeadlines(t *testing.T) {
	for _, test := range []struct {
		name                     string
		configured, parent, want time.Duration
	}{
		{"default", 0, 0, 30 * time.Second},
		{"mail timeout", 2 * time.Second, 0, 2 * time.Second},
		{"caller deadline", 30 * time.Second, time.Second, time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				sender, err := NewSender(config.MailConfig{Hostname: "mail.example.invalid", Port: 2525, Timeout: test.configured})
				if err != nil {
					t.Fatal(err)
				}
				sender.dialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				ctx := t.Context()
				if test.parent > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, test.parent)
					defer cancel()
				}
				start := time.Now()
				if err := sender.CheckHealth(ctx); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != test.want {
					t.Fatalf("error=%v elapsed=%s", err, time.Since(start))
				}
			})
		})
	}
}

func TestCheckHealthConcurrentCalls(t *testing.T) {
	sender, err := NewSender(config.MailConfig{Hostname: "mail.example.invalid", Port: 2525})
	if err != nil {
		t.Fatal(err)
	}
	var closed atomic.Int64
	sender.dialContext = func(context.Context, string, string) (net.Conn, error) {
		return probeConnection{close: func() error { closed.Add(1); return nil }}, nil
	}
	var calls sync.WaitGroup
	for range 16 {
		calls.Go(func() {
			if err := sender.CheckHealth(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	calls.Wait()
	if closed.Load() != 16 {
		t.Fatal("probe leaked connections")
	}
}
