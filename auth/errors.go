// errors.go defines the custom errors used by the auth pkg
package auth

import "fmt"

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

// ErrDatabaseUnavailable indicates the database is unavailable
type ErrLoadingTemplate struct {
	template string
	*BaseError
}

// NewErrDatabaseUnavailable creates an instance of ErrDatabaseUnavailable
func NewErrLoadingTemplate(err error, t string) *ErrLoadingTemplate {
	errorMessage := fmt.Sprintf("there was an error loading the specified template: %s", t)
	return &ErrLoadingTemplate{
		template: t,
		BaseError: &BaseError{
			Err:     err,
			Message: errorMessage,
		},
	}
}
