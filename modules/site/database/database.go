// Package database prepares the development site's SQLite schema.
package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
)

//go:embed schema.sql
var schema string

// PrepareDatabase creates the site's missing tables without changing existing data.
// The site owns its table names and schema. The caller owns the borrowed pool.
// This prepares the current schema only. It does not apply versioned migrations.
func PrepareDatabase(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("the database is not valid"))
	}

	if _, ok := db.Driver().(*sqlite3.SQLiteDriver); !ok {
		return database.NewErrDatabaseNotSupported(fmt.Errorf("the database type %T is not supported", db.Driver()))
	}

	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("prepare site schema: %w", err)
	}

	return nil
}
