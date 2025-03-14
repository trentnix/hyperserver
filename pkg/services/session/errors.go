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
