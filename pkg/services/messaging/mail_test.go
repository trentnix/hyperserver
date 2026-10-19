package messaging

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

type senderFunc func(context.Context, MailMessage) error

func (f senderFunc) Send(ctx context.Context, message MailMessage) error {
	return f(ctx, message)
}

func TestMailRejectsUnsafeHeaders(t *testing.T) {
	for _, field := range []string{"from", "to", "subject"} {
		for _, value := range []string{"person@example.invalid\r\nBcc: other@example.invalid", "value\nInjected: yes", "value\rInjected: yes", "value\x00", "value\x7f", "value\xff"} {
			t.Run(field+"/"+strconv.Quote(value), func(t *testing.T) {
				calls := 0
				client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
					calls++
					return nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				message := client.Compose().To("to@example.invalid").Subject("Test").Body("Hello")
				switch field {
				case "from":
					message.From(value)
				case "to":
					message.To(value)
				case "subject":
					message.Subject(value)
				}
				if err := message.Send(context.Background()); err == nil || calls != 0 {
					t.Fatalf("unsafe header: error=%v, sender calls=%d", err, calls)
				}
			})
		}
	}
}

func TestMailHonorsCanceledContext(t *testing.T) {
	called := false
	client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = client.Compose().To("to@example.invalid").Body("Hello").Send(ctx)
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled send: error=%v, sender called=%v", err, called)
	}
}

func TestMailRejectsInvalidAddresses(t *testing.T) {
	for _, value := range []string{"not-an-address", "a@example.invalid, b@example.invalid", "=?UTF-8?Q?name=0D=0ABcc=3A?= <a@example.invalid>"} {
		for _, field := range []string{"from", "to"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
					t.Error("sender called with invalid address")
					return nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				message := client.Compose().To("to@example.invalid").Body("Hello")
				if field == "from" {
					message.From(value)
				} else {
					message.To(value)
				}
				if err := message.Send(context.Background()); err == nil {
					t.Fatal("accepted invalid address")
				}
			})
		}
	}
}

func TestMailClientUsesInjectedSender(t *testing.T) {
	for _, from := range []string{"", "override@example.invalid"} {
		t.Run("from="+from, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := errors.New("delivery failed")
			calls := 0
			client, err := NewMailClientWithSender("default@example.invalid", senderFunc(func(gotCtx context.Context, message MailMessage) error {
				calls++
				wantFrom := from
				if wantFrom == "" {
					wantFrom = "default@example.invalid"
				}
				want := MailMessage{From: wantFrom, To: "recipient@example.invalid", Subject: "Test", Body: "<p>Hello</p>"}
				if message != want || gotCtx != ctx {
					t.Errorf("sender received message %+v and context %v, want %+v and original context", message, gotCtx, want)
				}
				return wantErr
			}))
			if err != nil {
				t.Fatal(err)
			}
			message := client.Compose().To("recipient@example.invalid").Subject("Test").Body("<p>Hello</p>")
			if from != "" {
				message.From(from)
			}
			if err := message.Send(ctx); !errors.Is(err, wantErr) {
				t.Fatalf("Send error = %v, want %v", err, wantErr)
			}
			if calls != 1 {
				t.Fatalf("sender calls = %d, want 1", calls)
			}
		})
	}
}

func TestMailClientRejectsIncompleteMessages(t *testing.T) {
	for _, tc := range []struct {
		name, from, to, body, want string
	}{
		{"from", "", "to@example.invalid", "Hello", "email cannot be sent without a from address"},
		{"to", "from@example.invalid", "", "Hello", "email cannot be sent without a to address"},
		{"body", "from@example.invalid", "to@example.invalid", "", "email cannot be sent without a body to render"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewMailClientWithSender(tc.from, senderFunc(func(context.Context, MailMessage) error {
				t.Fatal("sender called for an incomplete message")
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Compose().To(tc.to).Body(tc.body).Send(context.Background()); err == nil || err.Error() != tc.want {
				t.Fatalf("Send error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMailClientRequiresDependencies(t *testing.T) {
	client, err := NewMailClientWithSender("from@example.invalid", nil)
	if client != nil || err == nil {
		t.Fatalf("nil sender: client=%v error=%v", client, err)
	}
}

func TestMailClientReportsSenderOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"accepted", nil},
		{"unavailable", ErrMailUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
				calls++
				return tc.err
			}))
			if err != nil {
				t.Fatal(err)
			}
			err = client.Compose().To("to@example.invalid").Body("Hello").Send(context.Background())
			if !errors.Is(err, tc.err) || calls != 1 {
				t.Fatalf("Send = %v, calls = %d, want %v and one call", err, calls, tc.err)
			}
		})
	}
}
