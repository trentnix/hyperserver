package user

import (
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

// BaseError aliases the shared HyperServer error wrapper.
type BaseError = hs_errors.BaseError

// ErrUserNotSpecified indicates a user.User instance was required but was not set
type ErrUserNotSpecified struct {
	*BaseError
}

// NewErrUserNotSpecified returns an ErrUserNotSpecified wrapping err.
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

// NewErrUserNotFound returns an ErrUserNotFound wrapping err.
func NewErrUserNotFound(err error) *ErrUserNotFound {
	return &ErrUserNotFound{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to retrieve the specified user account",
		},
	}
}

// ErrUserChanged indicates that an update used an outdated account security version.
type ErrUserChanged struct {
	*BaseError
}

// NewErrUserChanged returns an ErrUserChanged wrapping err.
func NewErrUserChanged(err error) *ErrUserChanged {
	return &ErrUserChanged{
		BaseError: &BaseError{
			Err:     err,
			Message: "account security state changed since it was loaded",
		},
	}
}

// ErrToken indicates an error creating and managing a token
type ErrToken struct {
	*BaseError
}

// NewErrToken returns an ErrToken wrapping err.
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

// NewErrInvalidResetToken returns an ErrInvalidResetToken wrapping err.
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

// NewErrTokenNotSpecified returns an ErrTokenNotSpecified wrapping err.
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

// NewErrTokenNotFound returns an ErrTokenNotFound wrapping err.
func NewErrTokenNotFound(err error) *ErrTokenNotFound {
	return &ErrTokenNotFound{
		BaseError: &BaseError{
			Err:     err,
			Message: "token not found",
		},
	}
}

// ErrTokenExpired indicates that an account token has expired.
type ErrTokenExpired struct {
	*BaseError
}

// NewErrTokenExpired returns an ErrTokenExpired wrapping err.
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

// NewErrTokenExpirationNotSpecified returns an ErrTokenExpirationNotSpecified wrapping err.
func NewErrTokenExpirationNotSpecified(err error) *ErrTokenExpirationNotSpecified {
	return &ErrTokenExpirationNotSpecified{
		BaseError: &BaseError{
			Err:     err,
			Message: "token expiration not set",
		},
	}
}

// ErrSendVerification indicates there was an error sending a verification request
type ErrSendVerification struct {
	*BaseError
}

// NewErrSendVerification returns an ErrSendVerification wrapping err.
func NewErrSendVerification(err error) *ErrSendVerification {
	return &ErrSendVerification{
		BaseError: &BaseError{
			Err:     err,
			Message: "verification error",
		},
	}
}

// ErrJwtKeyNotSet indicates no JWT key was set
type ErrJwtKeyNotSet struct {
	*BaseError
}

// NewErrJwtKeyNotSet returns an ErrJwtKeyNotSet wrapping err.
func NewErrJwtKeyNotSet(err error) *ErrJwtKeyNotSet {
	return &ErrJwtKeyNotSet{
		BaseError: &BaseError{
			Err:     err,
			Message: "JWT key is not set",
		},
	}
}
