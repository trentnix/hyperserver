// errors.go defines custom errors in the content package
package session

// ErrSQLiteStoreNotCreated indicates a SQLite session store could not be created
type ErrSQLiteStoreNotCreated struct {
	*BaseError
}

// NewErrSQLiteStoreNotCreated creates an instance of ErrSQLiteStoreNotCreated
func NewErrSQLiteStoreNotCreated(err error) *ErrSQLiteStoreNotCreated {
	return &ErrSQLiteStoreNotCreated{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to create a Session store to store session data in a browser cookie",
		},
	}
}
