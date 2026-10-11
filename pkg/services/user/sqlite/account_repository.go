// Package sqlite provides account and token storage using a borrowed SQLite pool.
// Applications initialize the repository before use and close the pool after its consumers stop.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

// AccountRepository stores accounts using an existing SQLite pool.
// Call Initialize before use. The pool's owner must close it after consumers stop.
type AccountRepository struct{ db *sqlx.DB }

// NewAccountRepository borrows db. It does not open, initialize, or close storage.
func NewAccountRepository(db *sqlx.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

var _ accounts.AccountRepository = (*AccountRepository)(nil)
var _ accounts.TokenMaintenance = (*AccountRepository)(nil)

// ValidateOptions rejects account-specific settings. SQLite uses
// the application's shared database configuration.
func ValidateOptions(options map[string]string) error {
	if len(options) != 0 {
		return errors.New("auth.accountStorage.options: sqlite uses database configuration and accepts no options")
	}
	return nil
}

// DeleteExpiredTokens removes a bounded batch without initializing or closing storage.
func (s *AccountRepository) DeleteExpiredTokens(ctx context.Context, limit int) (int64, error) {
	return DeleteExpiredTokens(ctx, s.db, limit)
}

// Initialize prepares the account schema using the borrowed pool. It can be
// repeated or retried after failure and never closes the pool.
func (s *AccountRepository) Initialize(ctx context.Context) error {
	return PrepareDatabase(ctx, s.db)
}

// GetByID loads an account by ID, or returns [accounts.ErrUserNotFound].
func (s *AccountRepository) GetByID(ctx context.Context, id string) (*accounts.User, error) {
	if s.db == nil {
		return nil, database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	var u accounts.User
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
		return nil, accounts.NewErrUserNotFound(err)
	}
	if err != nil {
		return nil, err
	}

	return &u, nil
}

// GetByEmail loads an account by email, or returns [accounts.ErrUserNotFound].
func (s *AccountRepository) GetByEmail(ctx context.Context, email string) (*accounts.User, error) {
	if s.db == nil {
		return nil, database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}

	var user accounts.User
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
		return nil, accounts.NewErrUserNotFound(err)
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetToken reads stored metadata without consuming or checking the expiry of the token.
func (s *AccountRepository) GetToken(ctx context.Context, tokenHash, purpose string) (accounts.TokenMetadata, error) {
	if purpose != accounts.ResetTokenPurpose && purpose != accounts.VerificationTokenPurpose {
		return accounts.TokenMetadata{}, accounts.NewErrToken(errors.New("invalid token purpose"))
	}
	if tokenHash == "" {
		return accounts.TokenMetadata{}, accounts.NewErrTokenNotSpecified(fmt.Errorf("get token by value"))
	}
	if s.db == nil {
		return accounts.TokenMetadata{}, database.NewErrDatabaseUnavailable(nil)
	}

	var metadata accounts.TokenMetadata
	err := s.db.QueryRowContext(ctx, `SELECT user_id, token_hash, expires_at, token_type
		FROM `+userTokenTableName+` WHERE token_hash = ? AND token_type = ?`, tokenHash, purpose).
		Scan(&metadata.AccountID, &metadata.TokenHash, &metadata.ExpiresAt, &metadata.Purpose)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.TokenMetadata{}, accounts.NewErrTokenNotFound(fmt.Errorf("get token by value"))
	}
	if err != nil {
		return accounts.TokenMetadata{}, database.NewErrDatabase(fmt.Errorf("get token by value: %w", err))
	}
	return metadata, nil
}

// Create inserts a new account. An existing email returns [database.ErrRecordAlreadyExists].
// The supplied user is unchanged if insertion fails.
func (s *AccountRepository) Create(ctx context.Context, user *accounts.User) error {
	if user == nil {
		return accounts.NewErrUserNotSpecified(nil)
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

// InsertToken inserts token metadata without replacing or revoking existing tokens.
// The account schema must already exist. The raw token is never passed to storage.
func (s *AccountRepository) InsertToken(ctx context.Context, metadata accounts.TokenMetadata) error {
	if s.db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}
	errInsertToken := errors.New("error creating a new token in the database")
	if metadata.AccountID == "" {
		return accounts.NewErrUserNotSpecified(errInsertToken)
	}
	if metadata.TokenHash == "" {
		return accounts.NewErrTokenNotSpecified(errInsertToken)
	}
	if metadata.ExpiresAt.IsZero() {
		return accounts.NewErrTokenExpirationNotSpecified(errInsertToken)
	}
	if metadata.Purpose != accounts.ResetTokenPurpose && metadata.Purpose != accounts.VerificationTokenPurpose {
		return accounts.NewErrToken(errors.New("invalid token purpose"))
	}

	_, err := s.db.ExecContext(ctx, `INSERT INTO `+userTokenTableName+`
		(user_id, token_hash, expires_at, token_type) VALUES (?, ?, ?, ?)`,
		metadata.AccountID, metadata.TokenHash, metadata.ExpiresAt, metadata.Purpose)
	return err
}

// Update changes an existing account by ID. It never inserts an account and
// returns [accounts.ErrUserNotFound] if the ID no longer exists. Changing the email address
// revokes stored verification tokens in the same transaction. Changes to credentials,
// email, provider, or verification settings also invalidate authenticated sessions.
// The supplied user must carry the security version from Create or an account read.
// If security state has since changed, Update returns [accounts.ErrUserChanged] without
// changing the account, its tokens, or the supplied user. Reload the account and
// reapply the intended edit before retrying. Do not copy only the new version.
func (s *AccountRepository) Update(ctx context.Context, user *accounts.User) error {
	if user == nil {
		return accounts.NewErrUserNotSpecified(nil)
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
		user.ID, accounts.VerificationTokenPurpose, user.ID, user.Email)
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
		return accounts.NewErrUserNotFound(sql.ErrNoRows)
	}
	if err != nil {
		return err
	}
	if count == 0 {
		return accounts.NewErrUserChanged(nil)
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
func (s *AccountRepository) ChangePassword(ctx context.Context, u *accounts.User, passwordHash string) error {
	if u == nil || u.ID == "" {
		return accounts.NewErrUserNotSpecified(nil)
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
func (s *AccountRepository) ResetPassword(ctx context.Context, u *accounts.User, authorization accounts.ResetAuthorization, passwordHash string) error {
	if u == nil || u.ID == "" {
		return accounts.NewErrUserNotSpecified(nil)
	}
	if s.db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}
	if passwordHash == "" {
		return errors.New("a password hash is required")
	}
	if authorization.AccountID != u.ID || authorization.TokenHash == "" || authorization.ExpiresAt.IsZero() {
		return accounts.NewErrToken(errors.New("invalid reset authorization"))
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := deleteTokenAndCheckExpiry(ctx, tx, authorization.AccountID, authorization.TokenHash, accounts.ResetTokenPurpose, authorization.ExpiresAt); err != nil {
		return err
	}
	now := time.Now()

	// Only update the password. Do not overwrite unrelated account changes with
	// the snapshot used for form validation or the previous-password check.
	var updated accounts.User
	err = tx.GetContext(ctx, &updated, `UPDATE `+userTableName+` SET password = ?, updated_at = ?, session_version = session_version + 1
		WHERE id = ? AND password = ? AND registration_auth_type = ?
		RETURNING id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password, session_version`,
		passwordHash, now, u.ID, u.Password, u.RegistrationAuthType)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.NewErrToken(errors.New("account changed or no longer exists"))
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
func (s *AccountRepository) Verify(ctx context.Context, authorization accounts.VerificationAuthorization) (*accounts.User, error) {
	if s.db == nil {
		return nil, database.NewErrDatabaseUnavailable(nil)
	}
	if authorization.AccountID == "" || authorization.Email == "" || authorization.TokenHash == "" || authorization.ExpiresAt.IsZero() {
		return nil, accounts.NewErrToken(errors.New("invalid verification authorization"))
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := deleteTokenAndCheckExpiry(ctx, tx, authorization.AccountID, authorization.TokenHash, accounts.VerificationTokenPurpose, authorization.ExpiresAt); err != nil {
		return nil, err
	}

	var account accounts.User
	err = tx.GetContext(ctx, &account, `UPDATE `+userTableName+`
		SET session_version = session_version + CASE WHEN verified THEN 0 ELSE 1 END,
		verified = TRUE, updated_at = ? WHERE id = ? AND email = ?
		RETURNING id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password, session_version`,
		time.Now(), authorization.AccountID, authorization.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, accounts.NewErrToken(errors.New("verification address changed or account no longer exists"))
	}
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &account, nil
}

// deleteTokenAndCheckExpiry deletes the matching token, then checks its stored
// and signed expiry after acquiring the write lock. The caller must roll back on
// any error to restore the token.
func deleteTokenAndCheckExpiry(ctx context.Context, tx *sqlx.Tx, accountID, tokenHash, purpose string, signedExpiry time.Time) error {
	var expiresAt time.Time
	err := tx.QueryRowContext(ctx, `DELETE FROM `+userTokenTableName+`
		WHERE user_id = ? AND token_hash = ? AND token_type = ?
		RETURNING expires_at`, accountID, tokenHash, purpose).
		Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.NewErrTokenNotFound(err)
	}
	if err != nil {
		return err
	}

	now := time.Now()
	if !now.Before(expiresAt) || !now.Before(signedExpiry) {
		return accounts.NewErrTokenExpired(nil)
	}
	return nil
}
