// Package user stores accounts and account tokens and connects users to sessions.
// Schema preparation currently runs during account access and uses process-wide
// state. Reset and verification tokens have distinct signed purposes, but token
// consumption is not yet atomic and verification tokens remain reusable until expiry.
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

type (
	// User is a persisted account, including its authentication provider and verification state.
	// Password contains the provider's stored password hash, not plaintext.
	User struct {
		ID                   string    `db:"id"`
		Email                string    `db:"email"`
		Verified             bool      `db:"verified"`
		VerificationRequired bool      `db:"verification_required"`
		CreatedAt            time.Time `db:"created_at"`
		UpdatedAt            time.Time `db:"updated_at"`
		RegistrationAuthType string    `db:"registration_auth_type"`
		Password             string    `db:"password"`
	}

	contextKey string
)

const (
	// UserContextKey identifies the current User stored in a request context.
	UserContextKey contextKey = "auth-user"
)

// GetUserByID retrieves the user with the specified users.id value
func GetUserByID(db *sqlx.DB, id string) (*User, error) {
	if db == nil {
		return nil, database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	var u User
	if err := prepareDatabase(db); err != nil {
		return nil, err
	}

	if id == "" {
		return nil, fmt.Errorf("a users.id value must be specified to retrieve a user")
	}

	query := fmt.Sprintf(`
        SELECT id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password
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
	if err := prepareDatabase(db); err != nil {
		return nil, err
	}

	if email == "" {
		return nil, fmt.Errorf("a users.email value must be specified to retrieve a user")
	}

	query := fmt.Sprintf(`
        SELECT id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password
        FROM %s
        WHERE email = ?
    `, userTableName)

	err := db.Get(&user, query, email)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// Create inserts a new account. An existing email returns ErrRecordAlreadyExists
// from the database package. The receiver is unchanged if insertion fails.
func (user *User) Create(ctx context.Context, db *sqlx.DB) error {
	if user == nil {
		return NewErrUserNotSpecified(nil)
	}
	if user.ID != "" {
		return fmt.Errorf("a new user must not already have an id")
	}
	if user.Email == "" {
		return fmt.Errorf("a user email must be specified to create a user")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := prepareDatabase(db); err != nil {
		return err
	}

	created := *user
	created.ID = uuid.New().String()
	created.CreatedAt = time.Now()
	created.UpdatedAt = created.CreatedAt

	query := fmt.Sprintf(`
        INSERT INTO %s (id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password)
        VALUES (:id, :email, :verified, :verification_required, :created_at, :updated_at, :registration_auth_type, :password)
    `, userTableName)

	if _, err := db.NamedExecContext(ctx, query, &created); err != nil {
		return database.TranslateError(db.DB, err)
	}
	*user = created
	return nil
}

// Update changes an existing account by ID. It never inserts an account and
// returns ErrUserNotFound if the ID no longer exists.
func (user *User) Update(ctx context.Context, db *sqlx.DB) error {
	if user == nil {
		return NewErrUserNotSpecified(nil)
	}
	if user.ID == "" {
		return fmt.Errorf("a user.id value must be specified to update a user record")
	}
	if user.Email == "" {
		return fmt.Errorf("a user email must be specified to update a user")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := prepareDatabase(db); err != nil {
		return err
	}

	updated := *user
	updated.UpdatedAt = time.Now()

	query := fmt.Sprintf(`
        UPDATE %s
        SET email = :email,
            verified = :verified,
			verification_required = :verification_required,
            updated_at = :updated_at,
            registration_auth_type = :registration_auth_type,
            password = :password
        WHERE id = :id
    `, userTableName)

	result, err := db.NamedExecContext(ctx, query, &updated)
	if err != nil {
		return database.TranslateError(db.DB, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return NewErrUserNotFound(sql.ErrNoRows)
	}
	user.UpdatedAt = updated.UpdatedAt
	return nil
}

// Delete deletes the specified user from the users table
func (user *User) Delete(db *sqlx.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	if err := prepareDatabase(db); err != nil {
		return err
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

// NeedsVerification determines whether the specified user is not verified and needs to be
// verified to access resources available to authenticated, verified users
func (user *User) NeedsVerification() bool {
	if !user.VerificationRequired || user.Verified {
		return false
	}

	return true
}

// SetVerified sets the specified user's verification flag. The user will still need to be
// saved to serialize the change to the data store.
func (user *User) SetVerified() {
	user.Verified = true
}

// AddUserToRequestContext adds the specified user to the provided request
func AddUserToRequestContext(r *http.Request, user *User) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, UserContextKey, user)
	return r.WithContext(ctx)
}

// GetUserFromContext returns the User attached to ctx, or nil if none is present.
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
