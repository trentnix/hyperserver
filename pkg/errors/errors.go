// Package errors defines the common error wrapper used by HyperServer packages.
package errors

import "errors"

// WithCause preserves a public classification and a private cause for errors.Is
// and errors.As. Error returns only the public message. Logs can unwrap the cause.
// The public error must not be nil.
func WithCause(public, cause error) error {
	if cause == nil {
		return public
	}
	return &BaseError{Message: public.Error(), Err: cause, classification: public}
}

// BaseError pairs a public error message with an underlying cause.
type BaseError struct {
	classification error
	// Err is the cause returned by Unwrap.
	Err error
	// Message is the text returned by Error, without the cause's message.
	Message string
}

// Is matches the public classification while Unwrap retains the private cause.
func (e *BaseError) Is(target error) bool { return errors.Is(e.classification, target) }

// Error returns Message without appending the underlying cause.
func (e *BaseError) Error() string {
	return e.Message
}

// Unwrap returns Err for errors.Is and errors.As.
func (e *BaseError) Unwrap() error {
	return e.Err
}
