// Package database prepares the development site's SQLite schema.
package database

import (
	"database/sql"
	"fmt"

	"github.com/trentnix/hyperserver/pkg/database"
)

const (
	// MigrationsDirectory is the development site's file-based migration source.
	MigrationsDirectory = "file://modules/site/database/migrations"
)

// PrepareDatabase ensures that the module's database is configured correctly and is ready for use
func PrepareDatabase(db *sql.DB, contactSubmissionsTable string) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("the database is not valid"))
	}

	tableExists, err := database.TableExists(db, contactSubmissionsTable)
	if err != nil {
		return database.NewErrDatabase(err)
	}

	if !tableExists {
		err = database.RunMigrations(db, MigrationsDirectory)
		if err != nil {
			return database.NewErrDatabase(err)
		}
	}

	return nil
}
