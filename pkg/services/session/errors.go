package session

import "errors"

var (
	// ErrSessionManagerNotFound indicates that the request has no session manager.
	ErrSessionManagerNotFound = errors.New("the session manager is not configured")
	// ErrSessionStoreNotFound reports a missing session provider.
	ErrSessionStoreNotFound = errors.New("session store not configured for the specified session")
	// ErrStoreDisabled reports a disabled session provider.
	ErrStoreDisabled = errors.New("the specified session store is disabled")
	// ErrSessionInvalid reports invalid session data.
	ErrSessionInvalid = errors.New("the session is invalid")
	// ErrSessionCouldNotBeCreated reports failure to create a session.
	ErrSessionCouldNotBeCreated = errors.New("could not create a new session")
	// ErrSessionKeyInvalid reports an invalid session key.
	ErrSessionKeyInvalid = errors.New("the session key is invalid")
	// ErrInvalidToken reports an invalid session token.
	ErrInvalidToken = errors.New("the session token is invalid")
	// ErrTokenKeyNotSet reports a missing JWT signing key.
	ErrTokenKeyNotSet = errors.New("the JWT encryption key was not set")
	// ErrRequestNotSpecified reports a nil request.
	ErrRequestNotSpecified = errors.New("the http.Request is not specified")
)
