package errors

// BaseError provides common error functionality
type BaseError struct {
	Err     error
	Message string
}

// Error implements the error interface for BaseError
func (e *BaseError) Error() string {
	return e.Message
}

// Unwrap allows unwrapping the underlying error
func (e *BaseError) Unwrap() error {
	return e.Err
}
