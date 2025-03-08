// errors.go defines custom errors for the form package
package form

import (
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type BaseError = hs_errors.BaseError

// ErrInvalidForm indicates the form was not passed by reference
type ErrInvalidForm struct {
	*BaseError
}

// ErrFormComponentInterfaceNotImplemented indicates the form specified does not
// implement the FormComponent interface, which is required
type ErrFormComponentInterfaceNotImplemented struct {
	*BaseError
}

// NewErrInvalidForm creates an instance of ErrInvalidForm
func NewErrInvalidForm(err error) *ErrInvalidForm {
	return &ErrInvalidForm{
		BaseError: &BaseError{
			Err:     err,
			Message: "invalid form",
		},
	}
}

// NewErrFormComponentInterfaceNotImplemented creates an instance of ErrFormComponentInterfaceNotImplemented
func NewErrFormComponentInterfaceNotImplemented(err error) *ErrFormComponentInterfaceNotImplemented {
	return &ErrFormComponentInterfaceNotImplemented{
		BaseError: &BaseError{
			Err:     err,
			Message: "the specified form does not implement the FormComponent interface",
		},
	}
}
