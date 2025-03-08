// errors.go defines the custom errors used by the util package
package util

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

// ErrFailedToSetWorkingDirectory indicates the working directory could not be set
type ErrFailedToSetWorkingDirectory struct {
	*BaseError
}

// NewErrFailedToSetWorkingDirectory creates an instance of ErrFailedToSetWorkingDirectory
func NewErrFailedToSetWorkingDirectory(err error) *ErrFailedToSetWorkingDirectory {
	return &ErrFailedToSetWorkingDirectory{
		BaseError: &BaseError{
			Err:     err,
			Message: "failed to set the working directory",
		},
	}
}

// ErrWorkingDirectoryNotSpecified indicates a working directory value was not specified
type ErrWorkingDirectoryNotSpecified struct {
	*BaseError
}

// NewErrWorkingDirectoryNotSpecified creates an instance of ErrWorkingDirectoryNotSpecified
func NewErrWorkingDirectoryNotSpecified(err error) *ErrWorkingDirectoryNotSpecified {
	return &ErrWorkingDirectoryNotSpecified{
		BaseError: &BaseError{
			Err:     err,
			Message: "error configuring the working directory",
		},
	}
}

// ErrFailedToGetWorkingDirectory indicates the working directory could not be retrieved
type ErrFailedToGetWorkingDirectory struct {
	*BaseError
}

// NewErrFailedToSetWorkingDirectory creates an instance of ErrFailedToSetWorkingDirectory
func NewErrFailedToGetWorkingDirectory(err error) *ErrFailedToGetWorkingDirectory {
	return &ErrFailedToGetWorkingDirectory{
		BaseError: &BaseError{
			Err:     err,
			Message: "failed to retrieve the working directory",
		},
	}
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
