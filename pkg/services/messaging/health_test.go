package messaging

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

type healthSender struct {
	MailSender // Probing must not send mail.
	check      func(context.Context) error
}

func (s healthSender) CheckHealth(ctx context.Context) error { return s.check(ctx) }

func TestMailHealthOptionalProbe(t *testing.T) {
	client, err := NewMailClientWithSender("sender@example.com", senderFunc(func(context.Context, MailMessage) error {
		t.Fatal("probe sent a message")
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckHealth(t.Context()); err != nil {
		t.Fatal("sender without a probe:", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.CheckHealth(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled probe: %v", err)
	}
	for _, missing := range []*MailClient{nil, {}} {
		if err := missing.CheckHealth(t.Context()); !errors.Is(err, ErrMailUnavailable) {
			t.Fatalf("missing sender: %v", err)
		}
	}
}

func TestMailHealthDelegates(t *testing.T) {
	cause := errors.New("sender unavailable")
	for _, result := range []error{nil, cause} {
		calls := 0
		ctx := t.Context()
		client, err := NewMailClientWithSender("sender@example.com", healthSender{check: func(got context.Context) error {
			calls++
			if got != ctx {
				t.Fatal("probe received a different context")
			}
			return result
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.CheckHealth(ctx); !errors.Is(err, result) || calls != 1 {
			t.Fatalf("probe: error=%v calls=%d", err, calls)
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if err := client.CheckHealth(canceled); !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("canceled probe: error=%v calls=%d", err, calls)
		}
	}
}

func TestMailHealthHonorsDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := NewMailClientWithSender("sender@example.com", healthSender{check: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if err := client.CheckHealth(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timed-out probe: %v", err)
		}
	})
}
