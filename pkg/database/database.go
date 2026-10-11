// Package database opens SQL connection pools and resolves connection paths.
// Applications and storage providers import the drivers they use.
package database

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// Setup opens a sqlx connection pool for driver and connection.
// It does not ping the database, create tables, or seed data. The caller owns the pool.
// The driver must already be registered through its package import.
func Setup(driver string, connection string) (*sqlx.DB, error) {
	// Open the database using sqlx
	db, err := sqlx.Open(driver, connection)
	if err != nil {
		return nil, NewErrDatabaseConfiguration(fmt.Errorf("error opening database file: %w", err))
	}

	return db, nil
}
