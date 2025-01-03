// db_helper.go provides helpers for the current module to interact with the
// specified database
package models

import (
	"database/sql"
	"fmt"
	"net/mail"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/database"
)

// createContactSubmissionsTable creates a table to store ContactSubmissions objects
func createContactSubmissionsTable(db *sql.DB, tableName string) error {
	var createTableSQL string

	driver := db.Driver()

	switch driver.(type) {
	case *sqlite3.SQLiteDriver:
		createTableSQL = fmt.Sprintf(`
			CREATE TABLE %s (
				name TXT,
				email TEXT NOT NULL,
				message TEXT,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);`, tableName)
	default:
		return fmt.Errorf("Could not created '%s' table: Database driver not supported", tableName)
	}

	_, err := db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("Could not created '%s' table: %w", tableName, err)
	}

	return nil
}

// validateDatabase determines whether the database is valid for the module's functionality
// and, if not, it attempts to create the necessary database objects for the module to
// function correctly
func validateDatabase(db *sql.DB, contactsTable string) error {
	tableExists, err := database.TableExists(db, contactsTable)
	if err != nil {
		return err
	}

	if !tableExists {
		err = createContactSubmissionsTable(db, contactSubmissionsTable)
		if err != nil {
			return err
		}

	}

	return nil
}

// IsValidEmail confirms that the email provided is a validly constructed email address.
func IsValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}
