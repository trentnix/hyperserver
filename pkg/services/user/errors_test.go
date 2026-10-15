package user

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestIsTokenRejection(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"valid", nil, false},
		{"invalid", NewErrToken(nil), true},
		{"invalid reset", NewErrInvalidResetToken(nil), true},
		{"unspecified", NewErrTokenNotSpecified(nil), true},
		{"missing or consumed", NewErrTokenNotFound(nil), true},
		{"expired", NewErrTokenExpired(nil), true},
		{"missing account", NewErrUserNotFound(nil), true},
		{"changed account", NewErrUserChanged(nil), true},
		{"missing key", NewErrJwtKeyNotSet(nil), false},
		{"missing account argument", NewErrUserNotSpecified(nil), false},
		{"storage", errors.New("storage unavailable"), false},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTokenRejection(tc.err); got != tc.want {
				t.Fatalf("IsTokenRejection(%v)=%t, want %t", tc.err, got, tc.want)
			}
			if tc.err != nil && IsTokenRejection(fmt.Errorf("operation: %w", tc.err)) != tc.want {
				t.Fatal("wrapping changed the classification")
			}
		})
	}
}
