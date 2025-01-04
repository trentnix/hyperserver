// db_helper.go provides helpers for the current module to interact with the
// specified database
package database

import (
	"database/sql"
	"fmt"

	"github.com/trentnix/hyperserver/database"
)

const (
	MigrationsDirectory = "file://modules/site/database/migrations"
)

// PrepareDatabase ensures that the module's database is configured correctly and is ready for use
func PrepareDatabase(db *sql.DB, contactSubmissionsTable string) error {
	if db == nil {
		return fmt.Errorf("the database is not valid")
	}

	tableExists, err := database.TableExists(db, contactSubmissionsTable)
	if err != nil {
		return err
	}

	if !tableExists {
		err = database.RunMigrations(db, MigrationsDirectory)
		if err != nil {
			return err
		}

	}

	return nil
}
