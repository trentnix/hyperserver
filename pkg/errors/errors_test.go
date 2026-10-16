package errors

import (
	"errors"
	"fmt"
	"testing"
)

type privateFailure struct{ detail string }

func (e *privateFailure) Error() string { return e.detail }

func TestWithCausePreservesClassificationAndHidesDetails(t *testing.T) {
	public := errors.New("operation failed")
	private := &privateFailure{"password=private-value"}
	if got := WithCause(public, nil); got != public {
		t.Fatal("nil cause changed the sentinel")
	}
	wrapped := WithCause(public, private)
	if wrapped.Error() != public.Error() {
		t.Fatal("private detail appeared in public text")
	}
	for _, err := range []error{wrapped, fmt.Errorf("outer: %w", wrapped), errors.Join(errors.New("other"), wrapped)} {
		var cause *privateFailure
		if !errors.Is(err, public) || !errors.Is(err, private) || !errors.As(err, &cause) || cause != private {
			t.Fatalf("lost classification or cause: %v", err)
		}
	}
}
