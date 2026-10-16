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
		{"invalid", ErrToken, true},
		{"invalid reset", ErrInvalidResetToken, true},
		{"unspecified", ErrTokenNotSpecified, true},
		{"missing or consumed", ErrTokenNotFound, true},
		{"expired", ErrTokenExpired, true},
		{"missing account", ErrUserNotFound, true},
		{"changed account", ErrUserChanged, true},
		{"missing key", ErrJwtKeyNotSet, false},
		{"missing account argument", ErrUserNotSpecified, false},
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
