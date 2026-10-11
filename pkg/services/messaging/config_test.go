package messaging

import (
	"context"
	"errors"
	"testing"
)

type validatingSender struct {
	MailSender
	err   error
	calls int
}

func (s *validatingSender) ValidateConfig() error {
	s.calls++
	return s.err
}

func TestMailValidationUsesConfiguredSender(t *testing.T) {
	var absent *MailClient
	if err := absent.ValidateConfig(); !errors.Is(err, ErrMailUnavailable) {
		t.Fatalf("nil client error = %v", err)
	}
	if err := (&MailClient{}).ValidateConfig(); !errors.Is(err, ErrMailUnavailable) {
		t.Fatalf("missing sender error = %v", err)
	}
	client, err := NewMailClientWithSender("from@example.invalid", senderFunc(func(context.Context, MailMessage) error {
		t.Fatal("validation attempted delivery")
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ValidateConfig(); err != nil {
		t.Fatalf("configured replacement sender required SMTP settings: %v", err)
	}
	for _, validationErr := range []error{nil, ErrMailUnavailable} {
		sender := &validatingSender{err: validationErr}
		client, err := NewMailClientWithSender("from@example.invalid", sender)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.ValidateConfig(); !errors.Is(err, validationErr) || sender.calls != 1 {
			t.Fatalf("validation error = %v, calls = %d", err, sender.calls)
		}
	}
}
