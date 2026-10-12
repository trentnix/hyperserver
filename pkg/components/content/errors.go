package content

import hs_errors "github.com/trentnix/hyperserver/pkg/errors"

// BaseError aliases the shared HyperServer error wrapper.
type BaseError = hs_errors.BaseError

// ErrNoTemplates indicates no templates are selected for rendering
type ErrNoTemplates struct {
	*BaseError
}

// NewErrNoTemplates returns an ErrNoTemplates wrapping err.
func NewErrNoTemplates(err error) *ErrNoTemplates {
	return &ErrNoTemplates{
		BaseError: &BaseError{
			Err:     err,
			Message: "no templates specified",
		},
	}
}

// ErrParsingTemplates reports a failure parsing template files.
type ErrParsingTemplates struct {
	*BaseError
}

// NewErrParsingTemplates returns an ErrParsingTemplates wrapping err.
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

// NewErrRenderingTemplates returns an ErrRenderingTemplates wrapping err.
func NewErrRenderingTemplates(err error) *ErrRenderingTemplates {
	return &ErrRenderingTemplates{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to render the specified templates",
		},
	}
}
