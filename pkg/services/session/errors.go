// errors.go defines custom errors in the content package
package session

import (
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type BaseError = hs_errors.BaseError

// ErrSessionStoreNotFound a session store could not be found
type ErrSessionManagerNotFound struct {
	*BaseError
}

// NewErrSessionManagerNotFound creates an instance of ErrSessionManagerNotFound
func NewErrSessionManagerNotFound(err error) *ErrSessionManagerNotFound {
	return &ErrSessionManagerNotFound{
		BaseError: &BaseError{
			Err:     err,
			Message: "the session manager is not configured",
		},
	}
}

// ErrSessionStoreNotFound a session store could not be found
type ErrSessionStoreNotFound struct {
	*BaseError
}

// NewErrStoreNotFound creates an instance of ErrSessionStoreNotFound
func NewErrSessionStoreNotFound(err error) *ErrSessionStoreNotFound {
	return &ErrSessionStoreNotFound{
		BaseError: &BaseError{
			Err:     err,
			Message: "session store not configured for the specified session",
		},
	}
}

// ErrStoreDisabled identifies that a session store is disabled
type ErrStoreDisabled struct {
	*BaseError
}

// NewErrStoreDisabled creates an instance of ErrStoreDisabled
func NewErrStoreDisabled(err error) *ErrStoreDisabled {
	return &ErrStoreDisabled{
		BaseError: &BaseError{
			Err:     err,
			Message: "the specified session store is disabled",
		},
	}
}

// ErrSessionInvalid identifies that a session is not valid
type ErrSessionInvalid struct {
	*BaseError
}

// NewErrSessionInvalid creates an instance of ErrSessionInvalid
func NewErrSessionInvalid(err error) *ErrSessionInvalid {
	return &ErrSessionInvalid{
		BaseError: &BaseError{
			Err:     err,
			Message: "the session is invalid",
		},
	}
}

// ErrSessionCouldNotBeCreated is used when a session could not be created with the
// specified session store
type ErrSessionCouldNotBeCreated struct {
	*BaseError
}

// NewErrSessionCouldNotBeCreated creates an instance of ErrSessionCouldNotBeCreated
func NewErrSessionCouldNotBeCreated(err error) *ErrSessionCouldNotBeCreated {
	return &ErrSessionCouldNotBeCreated{
		BaseError: &BaseError{
			Err:     err,
			Message: "could not create a new session",
		},
	}
}

// ErrSessionKeyInvalid is used when a session key is invalid
type ErrSessionKeyInvalid struct {
	*BaseError
}

// NewErrSessionKeyInvalid creates an instance of ErrSessionKeyInvalid
func NewErrSessionKeyInvalid(err error) *ErrSessionKeyInvalid {
	return &ErrSessionKeyInvalid{
		BaseError: &BaseError{
			Err:     err,
			Message: "the session key is invalid",
		},
	}
}

// ErrInvalidToken is used when a the JWT token is invalid
type ErrInvalidToken struct {
	*BaseError
}

// NewErrInvalidToken creates an instance of ErrInvalidToken
func NewErrInvalidToken(err error) *ErrInvalidToken {
	return &ErrInvalidToken{
		BaseError: &BaseError{
			Err:     err,
			Message: "the session token is invalid",
		},
	}
}

// ErrTokenKeyNotSet is used when the JWT key is not set
type ErrTokenKeyNotSet struct {
	*BaseError
}

// NewErrTokenKeyNotSet creates an instance of ErrTokenKeyNotSet
func NewErrTokenKeyNotSet(err error) *ErrTokenKeyNotSet {
	return &ErrTokenKeyNotSet{
		BaseError: &BaseError{
			Err:     err,
			Message: "the JWT encryption key was not set",
		},
	}
}

// ErrCookieLifetimeNotSet is used when cookie lifetime is not set
type ErrCookieLifetimeNotSet struct {
	*BaseError
}

// NewErrCookieLifetimeNotSet creates an instance of ErrCookieLifetimeNotSet
func NewErrCookieLifetimeNotSet(err error) *ErrCookieLifetimeNotSet {
	return &ErrCookieLifetimeNotSet{
		BaseError: &BaseError{
			Err:     err,
			Message: "the lifetime of an HTTP cookie used to store session information is not set",
		},
	}
}

// ErrTokenLifetimeNotSet is used when the JWT key is not set
type ErrTokenLifetimeNotSet struct {
	*BaseError
}

// NewErrTokenLifetimeNotSet creates an instance of ErrTokenLifetimeNotSet
func NewErrTokenLifetimeNotSet(err error) *ErrTokenLifetimeNotSet {
	return &ErrTokenLifetimeNotSet{
		BaseError: &BaseError{
			Err:     err,
			Message: "the lifetime of a JWT token used to store session information is not set",
		},
	}
}
