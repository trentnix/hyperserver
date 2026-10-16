// Package database opens SQL connection pools and resolves connection paths.
// Applications and storage providers import the drivers they use.
package database

import (
	"database/sql"
	"fmt"

	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

// Setup opens a SQL connection pool for driver and connection.
// It does not ping the database, create tables, or seed data. The caller owns the pool.
// The driver must already be registered through its package import.
func Setup(driver string, connection string) (*sql.DB, error) {
	db, err := sql.Open(driver, connection)
	if err != nil {
		return nil, hs_errors.WithCause(ErrDatabaseConfiguration, fmt.Errorf("error opening database file: %w", err))
	}

	return db, nil
}
