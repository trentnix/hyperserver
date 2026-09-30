package session

// ErrCookieStoreNotCreated indicates a cookie-based session store could not be created
type ErrCookieStoreNotCreated struct {
	*BaseError
}

// NewErrCookieStoreNotCreated returns an ErrCookieStoreNotCreated wrapping err.
func NewErrCookieStoreNotCreated(err error) *ErrCookieStoreNotCreated {
	return &ErrCookieStoreNotCreated{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to create a Session store to store session data in a browser cookie",
		},
	}
}
