package user

import (
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type BaseError = hs_errors.BaseError

// ErrUserNotSpecified indicates a user.User instance was required but was not set
type ErrUserNotSpecified struct {
	*BaseError
}

// NewErrUserNotSpecified creates an instance of ErrUserNotSpecified
func NewErrUserNotSpecified(err error) *ErrUserNotSpecified {
	return &ErrUserNotSpecified{
		BaseError: &BaseError{
			Err:     err,
			Message: "a user account was not specified",
		},
	}
}

type ErrToken struct {
	*BaseError
}

// NewErrToken creates an instance of ErrDatabase
func NewErrToken(err error) *ErrToken {
	return &ErrToken{
		BaseError: &BaseError{
			Err:     err,
			Message: "token error",
		},
	}
}
