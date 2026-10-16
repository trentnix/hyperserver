package form

import "errors"

var (
	// ErrInvalidForm indicates a missing or unsupported form value.
	ErrInvalidForm = errors.New("invalid form")
)
