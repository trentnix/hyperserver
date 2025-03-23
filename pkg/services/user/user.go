// users.go defines the User model, database interactions, and serializes a user
// to and from a context
package user

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
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

const (
	UserContextKey contextKey = "auth-user"
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

	var u User
	if !databaseConfigured {
		err := prepareDatabase(db)
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

	err := db.Get(&u, query, id)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

// GetUserByEmail retrieves the user with the specified users.email value
func GetUserByEmail(db *sqlx.DB, email string) (*User, error) {
	if db == nil {
		return nil, database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	var user User
	if !databaseConfigured {
		err := prepareDatabase(db)
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

// Save serializes the specified user to the database. The database is checked
// to determine if it is configured and then whether a user with the specified email
// address is already stored in the database. If no user is found, a user record is created.
// If the user is found, the existing user record is updated.
func (user *User) Save(db *sqlx.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	if !databaseConfigured {
		err := prepareDatabase(db)
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

	if !databaseConfigured {
		err := prepareDatabase(db)
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

// AddUserToRequestContext adds the specified user to the provided request
func AddUserToRequestContext(r *http.Request, user *User) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, UserContextKey, user)
	return r.WithContext(ctx)
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
