package session

import (
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

// BaseError aliases the shared HyperServer error wrapper.
type BaseError = hs_errors.BaseError

// ErrSessionManagerNotFound indicates that the request has no session manager.
type ErrSessionManagerNotFound struct {
	*BaseError
}

// NewErrSessionManagerNotFound returns an ErrSessionManagerNotFound wrapping err.
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

// NewErrSessionStoreNotFound returns an ErrSessionStoreNotFound wrapping err.
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

// NewErrStoreDisabled returns an ErrStoreDisabled wrapping err.
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

// NewErrSessionInvalid returns an ErrSessionInvalid wrapping err.
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

// NewErrSessionCouldNotBeCreated returns an ErrSessionCouldNotBeCreated wrapping err.
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

// NewErrSessionKeyInvalid returns an ErrSessionKeyInvalid wrapping err.
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

// NewErrInvalidToken returns an ErrInvalidToken wrapping err.
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

// NewErrTokenKeyNotSet returns an ErrTokenKeyNotSet wrapping err.
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

// NewErrCookieLifetimeNotSet returns an ErrCookieLifetimeNotSet wrapping err.
func NewErrCookieLifetimeNotSet(err error) *ErrCookieLifetimeNotSet {
	return &ErrCookieLifetimeNotSet{
		BaseError: &BaseError{
			Err:     err,
			Message: "the lifetime of an HTTP cookie used to store session information is not set",
		},
	}
}

// ErrTokenLifetimeNotSet indicates that a session token lifetime is missing.
type ErrTokenLifetimeNotSet struct {
	*BaseError
}

// NewErrTokenLifetimeNotSet returns an ErrTokenLifetimeNotSet wrapping err.
func NewErrTokenLifetimeNotSet(err error) *ErrTokenLifetimeNotSet {
	return &ErrTokenLifetimeNotSet{
		BaseError: &BaseError{
			Err:     err,
			Message: "the lifetime of a JWT token used to store session information is not set",
		},
	}
}

// ErrRequestNotSpecified is used when a *http.Request is not specified (is nil)
type ErrRequestNotSpecified struct {
	*BaseError
}

// NewErrRequestNotSpecified returns an ErrRequestNotSpecified wrapping err.
func NewErrRequestNotSpecified(err error) *ErrRequestNotSpecified {
	return &ErrRequestNotSpecified{
		BaseError: &BaseError{
			Err:     err,
			Message: "the http.Request is not specified",
		},
	}
}
