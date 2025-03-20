package user

import (
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
)

const (
	userTableName      string = "user"
	userTokenTableName string = "usertoken"
)

var (
	dbMutex            sync.Mutex
	databaseConfigured bool
)

// validateDatabase determines whether the sessionsTable exists and, if not, it creates it
func prepareDatabase(db *sqlx.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	dbMutex.Lock()
	defer dbMutex.Unlock()

	// Check if the database has already been configured
	if databaseConfigured {
		return nil
	}

	// Attempt to configure the database
	userTableExists, err := database.TableExists(db.DB, userTableName)
	if err != nil {
		return database.NewErrDatabaseConfiguration(err)
	}

	if !userTableExists {
		if err = createUserTable(db); err != nil {
			return database.NewErrDatabase(err)
		}
	}

	// Ensure the second table is created, since it has a FK reference to the first table
	userTokenTableExists, err := database.TableExists(db.DB, userTokenTableName)
	if err != nil {
		return database.NewErrDatabaseConfiguration(err)
	}

	if !userTokenTableExists {
		if err = createUserTokenTable(db); err != nil {
			return database.NewErrDatabase(err)
		}
	}

	// Mark the database as configured so that subsequent calls can skip initialization
	databaseConfigured = true

	return nil
}

// createUserTable creates the user table according to the specified database vendor
func createUserTable(db *sqlx.DB) error {
	var createTableSQL string

	// Detect the database type by inspecting the driver
	driver := db.Driver()

	switch driver.(type) {
	case *sqlite3.SQLiteDriver:
		createTableSQL = fmt.Sprintf(`
			CREATE TABLE %s (
				id VARCHAR(36) PRIMARY KEY,
				email VARCHAR(255) NOT NULL UNIQUE,
				verified BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				registration_auth_type VARCHAR(50),
				password VARCHAR(255)
			);`, userTableName)
	default:
		return database.NewErrDatabaseNotSupported(fmt.Errorf("the database type %T is not supported", db.Driver()))
	}

	_, err := db.Exec(createTableSQL)
	if err != nil {
		return database.NewErrDatabase(err)
	}

	return nil
}

// createUserTokenTable creates the UserToken table according to the specified database vendor
func createUserTokenTable(db *sqlx.DB) error {
	var createTableSQL string

	// Detect the database type by inspecting the driver
	driver := db.Driver()

	switch driver.(type) {
	case *sqlite3.SQLiteDriver:
		createTableSQL = fmt.Sprintf(`
			CREATE TABLE %s (
				user_id VARCHAR(36),
				token_hash VARCHAR(255) NOT NULL,
				expires_at TIMESTAMP NOT NULL,
				token_type VARCHAR(255) NOT NULL, 
				PRIMARY KEY (user_id, token_hash),
				FOREIGN KEY (user_id) REFERENCES %s(id) ON DELETE CASCADE
			);`, userTokenTableName, userTableName)
	default:
		return database.NewErrDatabaseNotSupported(fmt.Errorf("the database type %T is not supported", db.Driver()))
	}

	_, err := db.Exec(createTableSQL)
	if err != nil {
		return database.NewErrDatabase(err)
	}

	return nil
}
