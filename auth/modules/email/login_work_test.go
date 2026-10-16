package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginPasswordWork(t *testing.T) {
	if cost, err := bcrypt.Cost([]byte(missingAccountHash)); err != nil || cost != bcrypt.DefaultCost {
		t.Fatalf("dummy hash cost = %d, error = %v", cost, err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(missingAccountHash), []byte("incorrect")); !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatalf("dummy hash is not usable: %v", err)
	}
	failure := errors.New("storage unavailable")
	for _, tc := range []struct {
		name       string
		account    *user.User
		lookupErr  error
		comparison error
		wantCalls  int
	}{
		{"missing", nil, user.ErrUserNotFound, nil, 1},
		{"nil account", nil, nil, nil, 1},
		{"no password", &user.User{}, nil, nil, 1},
		{"wrong password", &user.User{Password: "stored"}, nil, bcrypt.ErrMismatchedHashAndPassword, 1},
		{"corrupt hash", &user.User{Password: "corrupt"}, nil, bcrypt.ErrHashTooShort, 2},
		{"storage failure", nil, failure, nil, 0},
		{"cancellation", nil, context.Canceled, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &EmailAuthService{accounts: emailAccountRepository{lookup: func(context.Context, string) (*user.User, error) {
				return tc.account, tc.lookupErr
			}}}
			calls := 0
			got, err := service.authenticateWithPasswordCheck(t.Context(), "account@example.invalid", "supplied", func(hash, password []byte) error {
				calls++
				want := missingAccountHash
				if calls == 1 && tc.account != nil && tc.account.Password != "" {
					want = tc.account.Password
				}
				if string(hash) != want || string(password) != "supplied" {
					t.Fatal("wrong comparison inputs")
				}
				return tc.comparison
			})
			wantErr := errInvalidCredentials
			if tc.wantCalls == 0 {
				wantErr = tc.lookupErr
			}
			if got != nil || !errors.Is(err, wantErr) || calls != tc.wantCalls {
				t.Fatalf("account=%v error=%v calls=%d, want %v and %d calls", got, err, calls, wantErr, tc.wantCalls)
			}
		})
	}
}

func TestLoginRejectsOversizedPasswordBeforeLookup(t *testing.T) {
	service := &EmailAuthService{accounts: emailAccountRepository{lookup: func(context.Context, string) (*user.User, error) {
		t.Fatal("oversized password reached storage")
		return nil, nil
	}}}
	for _, value := range []string{strings.Repeat("a", 73), strings.Repeat("界", 25)} {
		got, err := service.authenticateWithPasswordCheck(t.Context(), "account@example.invalid", value, func([]byte, []byte) error {
			t.Fatal("oversized password reached bcrypt")
			return nil
		})
		if got != nil || !errors.Is(err, errInvalidCredentials) {
			t.Fatalf("result=%v, %v", got, err)
		}
	}
}
