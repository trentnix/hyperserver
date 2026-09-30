// Package errors defines the common error wrapper used by HyperServer packages.
package errors

// BaseError pairs a public error message with an underlying cause.
type BaseError struct {
	// Err is the cause returned by Unwrap.
	Err error
	// Message is the text returned by Error, without the cause's message.
	Message string
}

// Error returns Message without appending the underlying cause.
func (e *BaseError) Error() string {
	return e.Message
}

// Unwrap returns Err for errors.Is and errors.As.
func (e *BaseError) Unwrap() error {
	return e.Err
}
