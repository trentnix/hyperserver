// errors.go defines custom errors in the content package
package content

import (
	"fmt"

	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type BaseError = hs_errors.BaseError

// ErrRequestNotSet indicates a *http.Request instance is not set
type ErrRequestNotSet struct {
	*BaseError
}

// NewErrRenderingTemplates creates an instance of ErrRequestNotSet
func NewErrRequestNotSet(err error) *ErrRequestNotSet {
	return &ErrRequestNotSet{
		BaseError: &BaseError{
			Err:     err,
			Message: "the *http.Request was not set",
		},
	}
}

// ErrRequestNotSet indicates a *http.Request instance is not set
type ErrResourceNotFound struct {
	*BaseError
}

// NewErrResourceNotFound creates an instance of ErrResourceNotFound
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

// NewErrResourceNotFound creates an instance of ErrResourceNotFound
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

// NewErrContentManagerUnavailable creates an instance of ErrContentManagerUnavailable
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

// NewErrHandlerReferencesSelf creates an instance of ErrHandlerReferencesSelf
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
