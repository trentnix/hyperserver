package util

import (
	"fmt"

	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

// BaseError aliases the shared HyperServer error wrapper.
type BaseError = hs_errors.BaseError

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
