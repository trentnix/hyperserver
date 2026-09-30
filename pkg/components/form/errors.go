package form

import hs_errors "github.com/trentnix/hyperserver/pkg/errors"

// BaseError aliases the shared HyperServer error wrapper.
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

// NewErrInvalidForm returns an ErrInvalidForm wrapping err.
func NewErrInvalidForm(err error) *ErrInvalidForm {
	return &ErrInvalidForm{
		BaseError: &BaseError{
			Err:     err,
			Message: "invalid form",
		},
	}
}

// NewErrFormComponentInterfaceNotImplemented returns an ErrFormComponentInterfaceNotImplemented wrapping err.
func NewErrFormComponentInterfaceNotImplemented(err error) *ErrFormComponentInterfaceNotImplemented {
	return &ErrFormComponentInterfaceNotImplemented{
		BaseError: &BaseError{
			Err:     err,
			Message: "the specified form does not implement the FormComponent interface",
		},
	}
}
