package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestSingleAccount(t *testing.T) {
	db := userTestDB(t)
	accounts := NewAccountRepository(db)
	if got, err := accounts.SingleAccount(t.Context()); err != nil || got != nil {
		t.Fatalf("empty storage = %v, %v", got, err)
	}
	u := &user.User{Email: "owner@example.com", Password: "hash", RegistrationAuthType: "email"}
	if err := accounts.Create(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := accounts.SingleAccount(t.Context())
		if err != nil || got == nil || got.ID != u.ID || got.Email != u.Email || got.Password != u.Password || got.RegistrationAuthType != u.RegistrationAuthType || !got.CreatedAt.Equal(u.CreatedAt) || got.SessionVersion != u.SessionVersion {
			t.Fatalf("sole account = %v, %v", got, err)
		}
		got.Email = "changed@example.com"
	}
	if err := accounts.Create(t.Context(), &user.User{Email: "second@example.com"}); err != nil {
		t.Fatal(err)
	}
	if got, err := accounts.SingleAccount(t.Context()); err != nil || got != nil {
		t.Fatalf("multiple accounts = %v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := accounts.SingleAccount(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lookup = %v", err)
	}
	if _, err := NewAccountRepository(nil).SingleAccount(t.Context()); err == nil {
		t.Fatal("missing database did not fail")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.SingleAccount(t.Context()); err == nil {
		t.Fatal("closed database did not fail")
	}
}
