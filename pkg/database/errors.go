package database

import "errors"

var (
	// ErrDatabase wraps an error from a database operation.
	ErrDatabase = errors.New("database error")
	// ErrDatabaseUnavailable indicates the database is unavailable
	ErrDatabaseUnavailable = errors.New("the database is not available for use")
	// ErrDatabaseConfiguration indicates a configuration error
	ErrDatabaseConfiguration = errors.New("database configuration error")
	// ErrDatabaseNotSupported indicates the database is not supported
	ErrDatabaseNotSupported = errors.New("the database is not supported")
)
