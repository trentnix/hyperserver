package util

import (
	"fmt"

	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

// BaseError aliases the shared HyperServer error wrapper.
type BaseError = hs_errors.BaseError

// ErrFailedToSetWorkingDirectory indicates the working directory could not be set
type ErrFailedToSetWorkingDirectory struct {
	*BaseError
}

// NewErrFailedToSetWorkingDirectory returns an ErrFailedToSetWorkingDirectory wrapping err.
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

// NewErrWorkingDirectoryNotSpecified returns an ErrWorkingDirectoryNotSpecified wrapping err.
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

// NewErrFailedToGetWorkingDirectory returns an ErrFailedToGetWorkingDirectory wrapping err.
func NewErrFailedToGetWorkingDirectory(err error) *ErrFailedToGetWorkingDirectory {
	return &ErrFailedToGetWorkingDirectory{
		BaseError: &BaseError{
			Err:     err,
			Message: "failed to retrieve the working directory",
		},
	}
}

// ErrLoadingTemplate reports a failure loading a template file.
type ErrLoadingTemplate struct {
	template string
	*BaseError
}

// NewErrLoadingTemplate returns an ErrLoadingTemplate wrapping err.
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
