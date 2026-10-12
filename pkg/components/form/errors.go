package form

import hs_errors "github.com/trentnix/hyperserver/pkg/errors"

// BaseError aliases the shared HyperServer error wrapper.
type BaseError = hs_errors.BaseError

// ErrInvalidForm indicates a missing or unsupported form value.
type ErrInvalidForm struct {
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
