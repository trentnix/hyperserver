package session

// ErrSQLiteStoreNotCreated indicates a SQLite session store could not be created
type ErrSQLiteStoreNotCreated struct {
	*BaseError
}

// NewErrSQLiteStoreNotCreated returns an ErrSQLiteStoreNotCreated wrapping err.
func NewErrSQLiteStoreNotCreated(err error) *ErrSQLiteStoreNotCreated {
	return &ErrSQLiteStoreNotCreated{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to create a Session store to store session data in a browser cookie",
		},
	}
}
