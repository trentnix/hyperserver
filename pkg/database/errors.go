package database

import (
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

// BaseError aliases the shared HyperServer error wrapper.
type BaseError = hs_errors.BaseError

// ErrDatabase wraps an error from a database operation.
type ErrDatabase struct {
	*BaseError
}

// ErrDatabaseUnavailable indicates the database is unavailable
type ErrDatabaseUnavailable struct {
	*BaseError
}

// ErrDatabaseConfiguration indicates a configuration error
type ErrDatabaseConfiguration struct {
	*BaseError
}

// ErrDatabaseNotSupported indicates the database is not supported
type ErrDatabaseNotSupported struct {
	*BaseError
}

// ErrInvalidData indicates invalid data was provided
type ErrInvalidData struct {
	*BaseError
}

// NewErrDatabase returns an ErrDatabase wrapping err.
func NewErrDatabase(err error) *ErrDatabase {
	return &ErrDatabase{
		BaseError: &BaseError{
			Err:     err,
			Message: "database error",
		},
	}
}

// NewErrDatabaseUnavailable returns an ErrDatabaseUnavailable wrapping err.
func NewErrDatabaseUnavailable(err error) *ErrDatabaseUnavailable {
	return &ErrDatabaseUnavailable{
		BaseError: &BaseError{
			Err:     err,
			Message: "the database is not available for use",
		},
	}
}

// NewErrDatabaseConfiguration returns an ErrDatabaseConfiguration wrapping err.
func NewErrDatabaseConfiguration(err error) *ErrDatabaseConfiguration {
	return &ErrDatabaseConfiguration{
		BaseError: &BaseError{
			Err:     err,
			Message: "database configuration error",
		},
	}
}

// NewErrDatabaseNotSupported returns an ErrDatabaseNotSupported wrapping err.
func NewErrDatabaseNotSupported(err error) *ErrDatabaseNotSupported {
	return &ErrDatabaseNotSupported{
		BaseError: &BaseError{
			Err:     err,
			Message: "the database is not supported",
		},
	}
}

// NewErrInvalidData returns an ErrInvalidData wrapping err.
func NewErrInvalidData(err error) *ErrInvalidData {
	return &ErrInvalidData{
		BaseError: &BaseError{
			Err:     err,
			Message: "invalid data provided",
		},
	}
}
