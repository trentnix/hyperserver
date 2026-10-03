// Package database opens SQL connection pools and provides SQLite schema helpers.
// Setup does not create tables or verify connectivity.
package database

import (
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
	sqlite3 "github.com/mattn/go-sqlite3" // SQLite driver
)

// Setup opens a sqlx connection pool for driver and connection.
// It does not ping the database, create tables, or seed data. The caller owns the pool.
func Setup(driver string, connection string) (*sqlx.DB, error) {
	// Open the database using sqlx
	db, err := sqlx.Open(driver, connection)
	if err != nil {
		return nil, NewErrDatabaseConfiguration(fmt.Errorf("error opening database file: %w", err))
	}

	return db, nil
}

// TableExists reports whether a table exists in a SQLite database.
// Other database drivers return ErrDatabaseNotSupported.
func TableExists(db *sql.DB, tableName string) (bool, error) {
	var query string
	var args []interface{}

	driver := db.Driver()

	switch driver.(type) {
	case *sqlite3.SQLiteDriver:
		query = "SELECT name FROM sqlite_master WHERE type='table' AND name=?;"
		args = []interface{}{tableName}
	default:
		return false, NewErrDatabaseNotSupported(fmt.Errorf("the database type %T is not supported", db.Driver()))
	}

	var result string
	err := db.QueryRow(query, args...).Scan(&result)
	if err == sql.ErrNoRows {
		// table doesn't exist
		return false, nil
	} else if err != nil {
		return false, NewErrDatabase(fmt.Errorf("the was an error querying for the existence of table '%s': %w", tableName, err))
	}

	return true, nil
}
