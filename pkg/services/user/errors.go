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

// ErrUserNotFound indicates a user.User instance could not be retrieved
type ErrUserNotFound struct {
	*BaseError
}

// NewErrUserNotFound creates an instance of ErrUserNotFound
func NewErrUserNotFound(err error) *ErrUserNotFound {
	return &ErrUserNotFound{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to retrieve the specified user account",
		},
	}
}

// ErrToken indicates an error creating and managing a token
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

// ErrInvalidResetToken indicates an error creating and managing a reset token
type ErrInvalidResetToken struct {
	*BaseError
}

// NewErrInvalidResetToken creates an instance of ErrInvalidResetToken
func NewErrInvalidResetToken(err error) *ErrInvalidResetToken {
	return &ErrInvalidResetToken{
		BaseError: &BaseError{
			Err:     err,
			Message: "the reset token is invalid",
		},
	}
}

// ErrTokenNotSpecified indicates the token was not specified
type ErrTokenNotSpecified struct {
	*BaseError
}

// NewErrTokenNotSpecified creates an instance of ErrTokenNotSpecified
func NewErrTokenNotSpecified(err error) *ErrTokenNotSpecified {
	return &ErrTokenNotSpecified{
		BaseError: &BaseError{
			Err:     err,
			Message: "token not specified",
		},
	}
}

// ErrTokenNotFound indicates the token could not be retrieved
type ErrTokenNotFound struct {
	*BaseError
}

// NewErrTokenNotFound creates an instance of ErrTokenNotFound
func NewErrTokenNotFound(err error) *ErrTokenNotFound {
	return &ErrTokenNotFound{
		BaseError: &BaseError{
			Err:     err,
			Message: "token not found",
		},
	}
}

// ErrTokenExpired indicates an error creating and managing a reset token
type ErrTokenExpired struct {
	*BaseError
}

// NewErrTokenExpired creates an instance of ErrTokenExpired
func NewErrTokenExpired(err error) *ErrTokenExpired {
	return &ErrTokenExpired{
		BaseError: &BaseError{
			Err:     err,
			Message: "token expired",
		},
	}
}

// ErrTokenExpirationNotSpecified indicates no expiration value has been provided
type ErrTokenExpirationNotSpecified struct {
	*BaseError
}

// NewErrTokenExpiratioNotSpecified creates an instance of ErrTokenExpirationNotSpecified
func NewErrTokenExpirationNotSpecified(err error) *ErrTokenExpirationNotSpecified {
	return &ErrTokenExpirationNotSpecified{
		BaseError: &BaseError{
			Err:     err,
			Message: "token expiration not set",
		},
	}
}
