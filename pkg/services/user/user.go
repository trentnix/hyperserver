// Package user stores accounts and account tokens and connects users to sessions.
// Applications must call PrepareDatabase before accessing account storage.
// Password resets and email verification consume their exact stored token
// atomically with the account change.
package user

import (
	"context"
	"database/sql"
	"errors"
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
		// SessionVersion changes when credentials or account access changes.
		// Callers must preserve the version supplied by account reads and writes.
		SessionVersion int64 `db:"session_version"`
	}

	contextKey string
)

const (
	// UserContextKey identifies the current User stored in a request context.
	UserContextKey contextKey = "auth-user"
)

// GetUserByID retrieves the user with the specified users.id value
func GetUserByID(db *sqlx.DB, id string) (*User, error) {
	return GetUserByIDContext(context.Background(), db, id)
}

// GetUserByIDContext retrieves an account using ctx.
func GetUserByIDContext(ctx context.Context, db *sqlx.DB, id string) (*User, error) {
	if db == nil {
		return nil, database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	var u User
	if id == "" {
		return nil, fmt.Errorf("a users.id value must be specified to retrieve a user")
	}

	query := fmt.Sprintf(`
        SELECT id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password, session_version
        FROM %s
        WHERE id = ?
    `, userTableName)

	err := db.GetContext(ctx, &u, query, id)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

// GetUserByEmail retrieves the user with the specified users.email value
func GetUserByEmail(db *sqlx.DB, email string) (*User, error) {
	return GetUserByEmailContext(context.Background(), db, email)
}

// GetUserByEmailContext retrieves an account using ctx. The account schema must already exist.
func GetUserByEmailContext(ctx context.Context, db *sqlx.DB, email string) (*User, error) {
	if db == nil {
		return nil, database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	var user User
	if email == "" {
		return nil, fmt.Errorf("a users.email value must be specified to retrieve a user")
	}

	query := fmt.Sprintf(`
        SELECT id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password, session_version
        FROM %s
        WHERE email = ?
    `, userTableName)

	err := db.GetContext(ctx, &user, query, email)
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
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	created := *user
	created.ID = uuid.New().String()
	created.CreatedAt = time.Now()
	created.UpdatedAt = created.CreatedAt
	created.SessionVersion = 1

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
// returns ErrUserNotFound if the ID no longer exists. Changing the email address
// revokes stored verification tokens in the same transaction. Changes to credentials,
// email, provider, or verification settings also invalidate authenticated sessions.
// The receiver must carry the security version from Create or an account read.
// If security state has since changed, Update returns ErrUserChanged without
// changing the account, its tokens, or the receiver. Reload the account and
// reapply the intended edit before retrying. Do not copy only the new version.
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
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	updated := *user
	updated.UpdatedAt = time.Now()

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Revoke before updating so the comparison uses the stored email address.
	// Starting with a write also avoids upgrading a SQLite read transaction.
	_, err = tx.ExecContext(ctx, `DELETE FROM `+userTokenTableName+`
		WHERE user_id = ? AND token_type = ?
		AND EXISTS (SELECT 1 FROM `+userTableName+` WHERE id = ? AND email <> ?)`,
		user.ID, verificationTokenType, user.ID, user.Email)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
        UPDATE %s
        SET email = :email,
            verified = :verified,
			verification_required = :verification_required,
            updated_at = :updated_at,
            registration_auth_type = :registration_auth_type,
            session_version = session_version + CASE WHEN password IS NOT :password
                OR email IS NOT :email OR verified IS NOT :verified
                OR verification_required IS NOT :verification_required
                OR registration_auth_type IS NOT :registration_auth_type THEN 1 ELSE 0 END,
            password = :password
        WHERE id = :id AND session_version = :session_version
    `, userTableName)

	result, err := tx.NamedExecContext(ctx, query, &updated)
	if err != nil {
		return database.TranslateError(db.DB, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	err = tx.GetContext(ctx, &updated.SessionVersion, `SELECT session_version FROM user WHERE id = ?`, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return NewErrUserNotFound(sql.ErrNoRows)
	}
	if err != nil {
		return err
	}
	if count == 0 {
		return NewErrUserChanged(nil)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	user.UpdatedAt = updated.UpdatedAt
	user.SessionVersion = updated.SessionVersion
	return nil
}

// Delete deletes the specified user from the users table
func (user *User) Delete(db *sqlx.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
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
