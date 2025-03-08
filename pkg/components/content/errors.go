// errors.go defines custom errors in the content package
package content

import (
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type BaseError = hs_errors.BaseError

// ErrNoTemplates indicates no templates are selected for rendering
type ErrNoTemplates struct {
	*BaseError
}

// NewErrNoTemplates creates an instance of ErrNoTemplates
func NewErrNoTemplates(err error) *ErrNoTemplates {
	return &ErrNoTemplates{
		BaseError: &BaseError{
			Err:     err,
			Message: "no templates specified",
		},
	}
}

// ErrLoadingTemplates indicates an error parsing the specified templates
type ErrParsingTemplates struct {
	*BaseError
}

// NewErrNoTemplates creates an instance of ErrNoTemplates
func NewErrParsingTemplates(err error) *ErrParsingTemplates {
	return &ErrParsingTemplates{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to parse the specified templates",
		},
	}
}

// ErrRenderingTemplates indicates an error rendering the specified templates
type ErrRenderingTemplates struct {
	*BaseError
}

// NewErrRenderingTemplates creates an instance of ErrRenderingTemplates
func NewErrRenderingTemplates(err error) *ErrRenderingTemplates {
	return &ErrRenderingTemplates{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to render the specified templates",
		},
	}
}
