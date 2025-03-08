// users.go defines the User model
package user

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
)

// User stores an authenticated user, the type of authentication used, whether the user has been verified,
// and any password that may be used
type (
	User struct {
		ID                   string    `db:"id"`
		Email                string    `db:"email"`
		Verified             bool      `db:"verified"`
		CreatedAt            time.Time `db:"created_at"`
		UpdatedAt            time.Time `db:"updated_at"`
		RegistrationAuthType string    `db:"registration_auth_type"`
		Password             string    `db:"password"`
	}

	contextKey string
)

var (
	dbOnce              sync.Once
	userTableConfigured bool
)

const (
	UserContextKey contextKey = "auth-user"
	userTableName  string     = "user"
)

func NewUser() *User {
	return &User{
		Verified: false,
	}
}

// GetUserByID retrieves the user with the specified users.id value
func GetUserByID(db *sqlx.DB, id string) (*User, error) {
	if db == nil {
		return nil, database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	var hs_user User
	if !userTableConfigured {
		err := hs_user.prepareDatabase(db)
		if err != nil {
			return nil, err
		}
	}

	if id == "" {
		return nil, fmt.Errorf("a users.id value must be specified to retrieve a user")
	}

	query := fmt.Sprintf(`
        SELECT id, email, verified, created_at, updated_at, registration_auth_type, password
        FROM %s
        WHERE id = ?
    `, userTableName)

	err := db.Get(&hs_user, query, id)
	if err != nil {
		return nil, err
	}

	return &hs_user, nil
}

// GetUserByEmail retrieves the user with the specified users.email value
func GetUserByEmail(db *sqlx.DB, email string) (*User, error) {
	if db == nil {
		return nil, database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	var user User
	if !userTableConfigured {
		err := user.prepareDatabase(db)
		if err != nil {
			return nil, err
		}
	}

	if email == "" {
		return nil, fmt.Errorf("a users.email value must be specified to retrieve a user")
	}

	query := fmt.Sprintf(`
        SELECT id, email, verified, created_at, updated_at, registration_auth_type, password
        FROM %s
        WHERE email = ?
    `, userTableName)

	err := db.Get(&user, query, email)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// validateDatabase determines whether the sessionsTable exists and, if not, it creates it
func (user *User) prepareDatabase(db *sqlx.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	var dbError error
	dbOnce.Do(func() {
		tableExists, err := database.TableExists(db.DB, userTableName)
		if err != nil {
			dbError = database.NewErrDatabaseConfiguration(err)
			return
		}

		if !tableExists {
			err = user.createUserTable(db)
			if err != nil {
				dbError = database.NewErrDatabase(err)
				return
			}
		}

		userTableConfigured = true
	})

	if dbError != nil {
		return dbError
	}

	return nil
}

// configureDatabase creates the sessionTable according to the specified database vendor
func (u *User) createUserTable(db *sqlx.DB) error {
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

// Save serializes the specified user to the database. The database is checked
// to determine if it is configured and then whether a user with the specified email
// address is already stored in the database. If no user is found, a user record is created.
// If the user is found, the existing user record is updated.
func (user *User) Save(db *sqlx.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	if !userTableConfigured {
		err := user.prepareDatabase(db)
		if err != nil {
			return err
		}
	}

	dbUser, err := GetUserByEmail(db, user.Email)
	if err != sql.ErrNoRows && err != nil {
		// there was an error trying to retrieve the user
		return err
	}

	if dbUser == nil {
		// the user isn't found in the database - create it
		return user.create(db)
	}

	if user.ID == "" {
		user.ID = dbUser.ID
	}

	// the user record exists, so update it
	return user.update(db)
}

// create creates a new users table entry for the specified user.  Inputs aren't
// checked and the database configuration isn't validated since this is a non-exported
// method.
func (user *User) create(db *sqlx.DB) error {
	user.ID = uuid.New().String()
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	query := fmt.Sprintf(`
        INSERT INTO %s (id, email, verified, created_at, updated_at, registration_auth_type, password)
        VALUES (:id, :email, :verified, :created_at, :updated_at, :registration_auth_type, :password)
    `, userTableName)

	_, err := db.NamedExec(query, user)

	return err
}

// update updates the specified user in the users table. Inputs aren't checked and the
// database configuration isn't validated since this is a non-exported method.
func (user *User) update(db *sqlx.DB) error {
	if user.ID == "" {
		return fmt.Errorf("a user.id value must be specified to update a user record")
	}

	user.UpdatedAt = time.Now()

	query := fmt.Sprintf(`
        UPDATE %s
        SET email = :email,
            verified = :verified,
            updated_at = :updated_at,
            registration_auth_type = :registration_auth_type,
            password = :password
        WHERE id = :id
    `, userTableName)

	_, err := db.NamedExec(query, user)

	return err
}

// Delete deletes the specified user from the users table
func (user *User) Delete(db *sqlx.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	if !userTableConfigured {
		err := user.prepareDatabase(db)
		if err != nil {
			return err
		}
	}

	if user.ID == "" {
		return fmt.Errorf("a users.id value must be specified to delete a user")
	}

	query := fmt.Sprintf(`
        DELETE FROM %s
        WHERE id = ?
    `, userTableName)

	_, err := db.Exec(query, user.ID)
	return err
}

// AddUserToContext adds the specified user to the provided context
func AddUserToContext(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, UserContextKey, user)
}

// GetUserFromContext adds the specified user to the provided context
func GetUserFromContext(ctx context.Context) *User {
	if ctx == nil {
		return nil
	}

	user, ok := ctx.Value(UserContextKey).(*User)
	if !ok || user == nil {
		return nil
	}

	return user
}

// ClearUserFromContext deletes any user instance stored in the provided
// context and returns the updated context
func ClearUserFromContext(ctx context.Context) context.Context {
	// Set the user value to nil to indicate clearing the user
	return context.WithValue(ctx, UserContextKey, nil)
}
