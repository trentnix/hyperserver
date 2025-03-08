// errors.go defines the custom errors used by the database package
package database

import (
	"database/sql"
	"errors"

	"github.com/mattn/go-sqlite3"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type BaseError = hs_errors.BaseError

// ErrDatabaseUnavailable indicates the database is unavailable
type ErrDatabase struct {
	*BaseError
}

// ErrDatabaseUnavailable indicates the database is unavailable
type ErrDatabaseUnavailable struct {
	*BaseError
}

// ErrDatabaseConfiguration indicates a configuration error
type ErrDatabaseConfiguration struct {
	*BaseError
}

// ErrDatabaseNotSupported indicates the database is not supported
type ErrDatabaseNotSupported struct {
	*BaseError
}

type ErrDatabaseMigrationFailed struct {
	*BaseError
}

// ErrInvalidData indicates invalid data was provided
type ErrInvalidData struct {
	*BaseError
}

// ErrRecordAlreadyExists indicates the contact already exists
type ErrRecordAlreadyExists struct {
	*BaseError
}

// NewErrDatabase creates an instance of ErrDatabase
func NewErrDatabase(err error) *ErrDatabase {
	return &ErrDatabase{
		BaseError: &BaseError{
			Err:     err,
			Message: "database error",
		},
	}
}

// NewErrDatabaseUnavailable creates an instance of ErrDatabaseUnavailable
func NewErrDatabaseUnavailable(err error) *ErrDatabaseUnavailable {
	return &ErrDatabaseUnavailable{
		BaseError: &BaseError{
			Err:     err,
			Message: "the database is not available for use",
		},
	}
}

// NewErrDatabaseConfiguration creates an instance of ErrDatabaseConfiguration
func NewErrDatabaseConfiguration(err error) *ErrDatabaseConfiguration {
	return &ErrDatabaseConfiguration{
		BaseError: &BaseError{
			Err:     err,
			Message: "database configuration error",
		},
	}
}

// NewErrDatabaseNotSupported creates an instance of ErrDatabaseNotSupported
func NewErrDatabaseNotSupported(err error) *ErrDatabaseNotSupported {
	return &ErrDatabaseNotSupported{
		BaseError: &BaseError{
			Err:     err,
			Message: "the database is not supported",
		},
	}
}

func NewErrDatabaseMigrationFailed(err error) *ErrDatabaseMigrationFailed {
	return &ErrDatabaseMigrationFailed{
		BaseError: &BaseError{
			Err:     err,
			Message: "database migration failed",
		},
	}
}

// NewErrInvalidData creates an instance of ErrInvalidData
func NewErrInvalidData(err error) *ErrInvalidData {
	return &ErrInvalidData{
		BaseError: &BaseError{
			Err:     err,
			Message: "invalid data provided",
		},
	}
}

// NewErrContactAlreadyExists creates an instance of ErrContactAlreadyExists
func NewErrRecordAlreadyExists(err error) *ErrRecordAlreadyExists {
	return &ErrRecordAlreadyExists{
		BaseError: &BaseError{
			Err:     err,
			Message: "the contact already exists",
		},
	}
}

// translateError inspects the error depending on the database driver and provides
// a package-specific error type that can be inspected. This will prevent the caller
// from needing to be aware what database driver is being used.
func TranslateError(db *sql.DB, e error) error {
	if e != nil {
		// Detect the database type by inspecting the driver
		driver := db.Driver()

		switch driver.(type) {
		case *sqlite3.SQLiteDriver:
			return translateSQLiteError(e)
		default:
			return e
		}
	}

	return e
}

// translateSQLiteError takes the error provided and inspects it against sqlite3 package
// error types to identify what type of package-level error type it should be represented as
func translateSQLiteError(e error) error {
	var sqliteErr sqlite3.Error
	if errors.As(e, &sqliteErr) {
		if sqliteErr.Code == sqlite3.ErrConstraint && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			// Wrap the error with your custom error type
			return NewErrRecordAlreadyExists(e)
		}
	}

	return e
}
