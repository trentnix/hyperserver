// errors.go defines custom errors in the content package
package session

// ErrCookieStoreNotCreated indicates a cookie-based session store could not be created
type ErrCookieStoreNotCreated struct {
	*BaseError
}

// NewErrCookieStoreNotCreated creates an instance of ErrCookieStoreNotCreated
func NewErrCookieStoreNotCreated(err error) *ErrCookieStoreNotCreated {
	return &ErrCookieStoreNotCreated{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to create a Session store to store session data in a browser cookie",
		},
	}
}
