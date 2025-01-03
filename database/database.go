// database.go
package database

import (
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
	sqliteDriver "github.com/mattn/go-sqlite3" // SQLite driver

	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Setup initializes the database, creating tables and seeding data if necessary
func Setup(driver string, connection string) (*sqlx.DB, error) {
	// Open the database using sqlx
	db, err := sqlx.Open(driver, connection)
	if err != nil {
		return nil, fmt.Errorf("error opening database file: %w", err)
	}

	// Use the underlying *sql.DB for migration
	// sqlDriver, err := sqlite3.WithInstance(db.DB, &sqlite3.Config{})
	// if err != nil {
	// 	return nil, fmt.Errorf("could not start migration driver: %w", err)
	// }

	// Run migrations
	// m, err := migrate.NewWithDatabaseInstance(
	// 	"file://database/migrations",
	// 	driver, sqlDriver)
	// if err != nil {
	// 	return nil, fmt.Errorf("migration failed: %w", err)
	// }

	// // Apply migrations
	// if err := m.Up(); err != nil && err != migrate.ErrNoChange {
	// 	return nil, fmt.Errorf("could not apply migrations: %w", err)
	// }

	return db, nil
}

// TableExists determines whether the table specified by tableName exists
func TableExists(db *sql.DB, tableName string) (bool, error) {
	var query string
	var args []interface{}

	driver := db.Driver()

	switch driver.(type) {
	case *sqliteDriver.SQLiteDriver:
		query = "SELECT name FROM sqlite_master WHERE type='table' AND name=?;"
		args = []interface{}{tableName}
	default:
		return false, fmt.Errorf("the database type %T is not supported", db.Driver())
	}

	var result string
	err := db.QueryRow(query, args...).Scan(&result)
	if err == sql.ErrNoRows {
		// table doesn't exist
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("the was an error querying for the existence of table '%s': %w", tableName, err)
	}

	return true, nil
}
