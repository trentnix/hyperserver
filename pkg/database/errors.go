package database

import (
	"database/sql"
	"errors"

	"github.com/mattn/go-sqlite3"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

// BaseError aliases the shared HyperServer error wrapper.
type BaseError = hs_errors.BaseError

// ErrDatabase wraps an error from a database operation.
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

// ErrDatabaseMigrationFailed reports a failure creating or running a migration.
type ErrDatabaseMigrationFailed struct {
	*BaseError
}

// ErrInvalidData indicates invalid data was provided
type ErrInvalidData struct {
	*BaseError
}

// ErrRecordAlreadyExists indicates a record violates a uniqueness constraint.
type ErrRecordAlreadyExists struct {
	*BaseError
}

// NewErrDatabase returns an ErrDatabase wrapping err.
func NewErrDatabase(err error) *ErrDatabase {
	return &ErrDatabase{
		BaseError: &BaseError{
			Err:     err,
			Message: "database error",
		},
	}
}

// NewErrDatabaseUnavailable returns an ErrDatabaseUnavailable wrapping err.
func NewErrDatabaseUnavailable(err error) *ErrDatabaseUnavailable {
	return &ErrDatabaseUnavailable{
		BaseError: &BaseError{
			Err:     err,
			Message: "the database is not available for use",
		},
	}
}

// NewErrDatabaseConfiguration returns an ErrDatabaseConfiguration wrapping err.
func NewErrDatabaseConfiguration(err error) *ErrDatabaseConfiguration {
	return &ErrDatabaseConfiguration{
		BaseError: &BaseError{
			Err:     err,
			Message: "database configuration error",
		},
	}
}

// NewErrDatabaseNotSupported returns an ErrDatabaseNotSupported wrapping err.
func NewErrDatabaseNotSupported(err error) *ErrDatabaseNotSupported {
	return &ErrDatabaseNotSupported{
		BaseError: &BaseError{
			Err:     err,
			Message: "the database is not supported",
		},
	}
}

// NewErrDatabaseMigrationFailed returns an ErrDatabaseMigrationFailed wrapping err.
func NewErrDatabaseMigrationFailed(err error) *ErrDatabaseMigrationFailed {
	return &ErrDatabaseMigrationFailed{
		BaseError: &BaseError{
			Err:     err,
			Message: "database migration failed",
		},
	}
}

// NewErrInvalidData returns an ErrInvalidData wrapping err.
func NewErrInvalidData(err error) *ErrInvalidData {
	return &ErrInvalidData{
		BaseError: &BaseError{
			Err:     err,
			Message: "invalid data provided",
		},
	}
}

// NewErrRecordAlreadyExists returns an ErrRecordAlreadyExists wrapping err.
func NewErrRecordAlreadyExists(err error) *ErrRecordAlreadyExists {
	return &ErrRecordAlreadyExists{
		BaseError: &BaseError{
			Err:     err,
			Message: "the record already exists",
		},
	}
}

// TranslateError wraps SQLite uniqueness failures in ErrRecordAlreadyExists.
// It returns nil or unrecognized errors unchanged.
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
