package content

import (
	"fmt"

	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

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

// ErrRequestNotSet indicates a *http.Request instance is not set
type ErrRequestNotSet struct {
	*BaseError
}

// NewErrRequestNotSet returns an ErrRequestNotSet wrapping err.
func NewErrRequestNotSet(err error) *ErrRequestNotSet {
	return &ErrRequestNotSet{
		BaseError: &BaseError{
			Err:     err,
			Message: "the *http.Request was not set",
		},
	}
}

// ErrResourceNotFound indicates that a requested resource is unavailable.
type ErrResourceNotFound struct {
	*BaseError
}

// NewErrResourceNotFound wraps err with a resource-not-found message.
// Despite its name, it currently returns ErrRequestNotSet, not ErrResourceNotFound.
func NewErrResourceNotFound(err error) *ErrRequestNotSet {
	return &ErrRequestNotSet{
		BaseError: &BaseError{
			Err:     err,
			Message: "the specified resource was not found",
		},
	}
}

// ErrHtmxRequestRequired indicates an HTMX request is required
type ErrHtmxRequestRequired struct {
	*BaseError
}

// NewErrHtmxRequestRequired returns an ErrHtmxRequestRequired wrapping err.
func NewErrHtmxRequestRequired(err error) *ErrHtmxRequestRequired {
	return &ErrHtmxRequestRequired{
		BaseError: &BaseError{
			Err:     err,
			Message: "the specified request must be made via HTMX",
		},
	}
}

// ErrContentManagerUnavailable indicates the content manager is not set or is
// not available
type ErrContentManagerUnavailable struct {
	*BaseError
}

// NewErrContentManagerUnavailable returns an ErrContentManagerUnavailable wrapping err.
func NewErrContentManagerUnavailable(err error) *ErrContentManagerUnavailable {
	return &ErrContentManagerUnavailable{
		BaseError: &BaseError{
			Err:     err,
			Message: "the content manager is not available",
		},
	}
}

// ErrHandlerReferencesSelf indicates the content manager's handler value is the current function,
// which will result in an infinite loop
type ErrHandlerReferencesSelf struct {
	Handler string
	*BaseError
}

// NewErrHandlerReferencesSelf returns an ErrHandlerReferencesSelf wrapping err.
func NewErrHandlerReferencesSelf(err error, handler string) *ErrHandlerReferencesSelf {
	errMessage := fmt.Sprintf("the specified handler '%s' is also the content manager handler", handler)
	return &ErrHandlerReferencesSelf{
		Handler: handler,
		BaseError: &BaseError{
			Err:     err,
			Message: errMessage,
		},
	}
}
