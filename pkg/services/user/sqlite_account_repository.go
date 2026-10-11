package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
)

// SQLiteAccountRepository stores accounts using an existing SQLite pool.
// Call Initialize before use. The pool's owner must close it after consumers stop.
type SQLiteAccountRepository struct{ db *sqlx.DB }

// NewSQLiteAccountRepository borrows db. It does not open, initialize, or close storage.
func NewSQLiteAccountRepository(db *sqlx.DB) *SQLiteAccountRepository {
	return &SQLiteAccountRepository{db: db}
}

var _ AccountRepository = (*SQLiteAccountRepository)(nil)

// ValidateSQLiteAccountOptions rejects account-specific settings. SQLite uses
// the application's shared database configuration.
func ValidateSQLiteAccountOptions(options map[string]string) error {
	if len(options) != 0 {
		return errors.New("auth.accountStorage.options: sqlite uses database configuration and accepts no options")
	}
	return nil
}

// DeleteExpiredTokens removes a bounded batch without initializing or closing storage.
func (s *SQLiteAccountRepository) DeleteExpiredTokens(ctx context.Context, limit int) (int64, error) {
	return DeleteExpiredTokens(ctx, s.db, limit)
}

// Initialize prepares the account schema using the borrowed pool. It can be
// repeated or retried after failure and never closes the pool.
func (s *SQLiteAccountRepository) Initialize(ctx context.Context) error {
	return PrepareDatabase(ctx, s.db)
}

// GetByID loads an account by ID, or returns ErrUserNotFound.
func (s *SQLiteAccountRepository) GetByID(ctx context.Context, id string) (*User, error) {
	if s.db == nil {
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

	err := s.db.GetContext(ctx, &u, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewErrUserNotFound(err)
	}
	if err != nil {
		return nil, err
	}

	return &u, nil
}

// GetByEmail loads an account by email, or returns ErrUserNotFound.
func (s *SQLiteAccountRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	if s.db == nil {
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

	err := s.db.GetContext(ctx, &user, query, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewErrUserNotFound(err)
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetToken reads stored metadata without consuming or checking the expiry of the token.
func (s *SQLiteAccountRepository) GetToken(ctx context.Context, tokenHash, purpose string) (TokenMetadata, error) {
	if purpose != resetTokenType && purpose != verificationTokenType {
		return TokenMetadata{}, NewErrToken(errors.New("invalid token purpose"))
	}
	token, err := getTokenByHashContext(ctx, s.db, tokenHash, purpose)
	if err != nil {
		return TokenMetadata{}, err
	}
	return token.Metadata(), nil
}

// Create inserts a new account. An existing email returns ErrRecordAlreadyExists
// from the database package. The supplied user is unchanged if insertion fails.
func (s *SQLiteAccountRepository) Create(ctx context.Context, user *User) error {
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
	if s.db == nil {
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

	if _, err := s.db.NamedExecContext(ctx, query, &created); err != nil {
		return database.TranslateError(s.db.DB, err)
	}
	*user = created
	return nil
}

// CreateToken inserts token metadata without replacing or revoking existing tokens.
func (s *SQLiteAccountRepository) CreateToken(ctx context.Context, metadata TokenMetadata) error {
	token := AuthToken{
		UserId: metadata.AccountID, TokenHash: metadata.TokenHash,
		Type: metadata.Purpose, ExpiresAt: metadata.ExpiresAt,
	}
	return token.CreateContext(ctx, s.db)
}

// Update changes an existing account by ID. It never inserts an account and
// returns ErrUserNotFound if the ID no longer exists. Changing the email address
// revokes stored verification tokens in the same transaction. Changes to credentials,
// email, provider, or verification settings also invalidate authenticated sessions.
// The supplied user must carry the security version from Create or an account read.
// If security state has since changed, Update returns ErrUserChanged without
// changing the account, its tokens, or the supplied user. Reload the account and
// reapply the intended edit before retrying. Do not copy only the new version.
func (s *SQLiteAccountRepository) Update(ctx context.Context, user *User) error {
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
	if s.db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	updated := *user
	updated.UpdatedAt = time.Now()

	tx, err := s.db.BeginTxx(ctx, nil)
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
		return database.TranslateError(s.db.DB, err)
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

// ChangePassword stores a validated password hash and invalidates all existing
// authenticated sessions. A concurrent account change rejects the operation.
// The supplied user changes only after the database update succeeds.
func (s *SQLiteAccountRepository) ChangePassword(ctx context.Context, u *User, passwordHash string) error {
	if u == nil || u.ID == "" {
		return NewErrUserNotSpecified(nil)
	}
	if s.db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}
	if passwordHash == "" {
		return errors.New("a password hash is required")
	}

	now := time.Now()
	result, err := s.db.ExecContext(ctx, `UPDATE user
		SET password = ?, updated_at = ?, session_version = session_version + 1
		WHERE id = ? AND password = ? AND registration_auth_type = ? AND session_version = ?`,
		passwordHash, now, u.ID, u.Password, u.RegistrationAuthType, u.SessionVersion)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("account changed or no longer exists")
	}

	u.Password = passwordHash
	u.UpdatedAt = now
	u.SessionVersion++
	return nil
}

// ResetPassword consumes a validated reset token and updates the password in one
// transaction. The caller must check the signature before supplying authorization.
func (s *SQLiteAccountRepository) ResetPassword(ctx context.Context, u *User, authorization ResetAuthorization, passwordHash string) error {
	if u == nil || u.ID == "" {
		return NewErrUserNotSpecified(nil)
	}
	if s.db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}
	if passwordHash == "" {
		return errors.New("a password hash is required")
	}
	if authorization.AccountID != u.ID || authorization.TokenHash == "" || authorization.ExpiresAt.IsZero() {
		return NewErrToken(errors.New("invalid reset authorization"))
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := consumeStoredToken(ctx, tx, authorization.AccountID, authorization.TokenHash, resetTokenType, authorization.ExpiresAt); err != nil {
		return err
	}
	now := time.Now()

	// Only update the password. Do not overwrite unrelated account changes with
	// the snapshot used for form validation or the previous-password check.
	var updated User
	err = tx.GetContext(ctx, &updated, `UPDATE `+userTableName+` SET password = ?, updated_at = ?, session_version = session_version + 1
		WHERE id = ? AND password = ? AND registration_auth_type = ?
		RETURNING id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password, session_version`,
		passwordHash, now, u.ID, u.Password, u.RegistrationAuthType)
	if errors.Is(err, sql.ErrNoRows) {
		return NewErrToken(errors.New("account changed or no longer exists"))
	}
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	*u = updated
	return nil
}

// Verify consumes a validated verification token and verifies its email address
// in one transaction. The caller must check the signature before supplying authorization.
func (s *SQLiteAccountRepository) Verify(ctx context.Context, authorization VerificationAuthorization) (*User, error) {
	if s.db == nil {
		return nil, database.NewErrDatabaseUnavailable(nil)
	}
	if authorization.AccountID == "" || authorization.Email == "" || authorization.TokenHash == "" || authorization.ExpiresAt.IsZero() {
		return nil, NewErrToken(errors.New("invalid verification authorization"))
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := consumeStoredToken(ctx, tx, authorization.AccountID, authorization.TokenHash, verificationTokenType, authorization.ExpiresAt); err != nil {
		return nil, err
	}

	var account User
	err = tx.GetContext(ctx, &account, `UPDATE `+userTableName+`
		SET session_version = session_version + CASE WHEN verified THEN 0 ELSE 1 END,
		verified = TRUE, updated_at = ? WHERE id = ? AND email = ?
		RETURNING id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password, session_version`,
		time.Now(), authorization.AccountID, authorization.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewErrToken(errors.New("verification address changed or account no longer exists"))
	}
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &account, nil
}
