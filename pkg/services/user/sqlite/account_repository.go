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
	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

// AccountRepository stores accounts using an existing SQLite pool.
// Call Initialize before use. The pool's owner must close it after consumers stop.
type AccountRepository struct{ db *sql.DB }

// NewAccountRepository borrows db. It does not open, initialize, or close storage.
func NewAccountRepository(db *sql.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

// accountColumns and scanAccount must use the same field order.
const accountColumns = "id, email, verified, verification_required, created_at, updated_at, registration_auth_type, password, session_version, role, invited"

func scanAccount(row interface{ Scan(...any) error }) (*accounts.User, error) {
	var u accounts.User
	if err := row.Scan(
		&u.ID, &u.Email, &u.Verified, &u.VerificationRequired,
		&u.CreatedAt, &u.UpdatedAt, &u.RegistrationAuthType, &u.Password, &u.SessionVersion, &u.Role, &u.Invited,
	); err != nil {
		return nil, err
	}
	return &u, nil
}

var _ accounts.AccountRepository = (*AccountRepository)(nil)
var _ accounts.TokenMaintenance = (*AccountRepository)(nil)
var _ accounts.SingleAccountReader = (*AccountRepository)(nil)

// SingleAccount returns the sole account without counting the entire table.
func (s *AccountRepository) SingleAccount(ctx context.Context) (*accounts.User, error) {
	if s.db == nil {
		return nil, database.ErrDatabaseUnavailable
	}
	query := `SELECT ` + accountColumns + ` FROM user
		WHERE (SELECT count(*) FROM (SELECT 1 FROM user LIMIT 2)) = 1 LIMIT 1`
	u, err := scanAccount(s.db.QueryRowContext(ctx, query))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
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

// CheckHealth checks that the account table can be read without loading an account.
// It is safe for concurrent calls and honors ctx.
func (s *AccountRepository) CheckHealth(ctx context.Context) error {
	if s.db == nil {
		return database.ErrDatabaseUnavailable
	}
	var count int
	return s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM (SELECT 1 FROM "+userTableName+" LIMIT 1)").Scan(&count)
}

// GetByID loads an account by ID, or returns [accounts.ErrUserNotFound].
func (s *AccountRepository) GetByID(ctx context.Context, id string) (*accounts.User, error) {
	if s.db == nil {
		return nil, hs_errors.WithCause(database.ErrDatabaseUnavailable, fmt.Errorf("no database connection is specified"))
	}

	if id == "" {
		return nil, fmt.Errorf("a users.id value must be specified to retrieve a user")
	}

	query := `SELECT ` + accountColumns + ` FROM ` + userTableName + ` WHERE id = ?`

	u, err := scanAccount(s.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, hs_errors.WithCause(accounts.ErrUserNotFound, err)
	}
	if err != nil {
		return nil, err
	}

	return u, nil
}

// GetByEmail loads an account using [accounts.EmailKey], or returns [accounts.ErrUserNotFound].
func (s *AccountRepository) GetByEmail(ctx context.Context, email string) (*accounts.User, error) {
	if s.db == nil {
		return nil, hs_errors.WithCause(database.ErrDatabaseUnavailable, fmt.Errorf("no database connection is specified"))
	}

	if email == "" {
		return nil, fmt.Errorf("a users.email value must be specified to retrieve a user")
	}

	query := `SELECT ` + accountColumns + ` FROM ` + userTableName + ` WHERE email_key = ?`

	user, err := scanAccount(s.db.QueryRowContext(ctx, query, accounts.EmailKey(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, hs_errors.WithCause(accounts.ErrUserNotFound, err)
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetToken reads stored metadata without consuming or checking the expiry of the token.
func (s *AccountRepository) GetToken(ctx context.Context, tokenHash, purpose string) (accounts.TokenMetadata, error) {
	if purpose != accounts.ResetTokenPurpose && purpose != accounts.VerificationTokenPurpose && purpose != accounts.PasswordSetupTokenPurpose && purpose != accounts.InvitationTokenPurpose {
		return accounts.TokenMetadata{}, hs_errors.WithCause(accounts.ErrToken, errors.New("invalid token purpose"))
	}
	if tokenHash == "" {
		return accounts.TokenMetadata{}, hs_errors.WithCause(accounts.ErrTokenNotSpecified, fmt.Errorf("get token by value"))
	}
	if s.db == nil {
		return accounts.TokenMetadata{}, database.ErrDatabaseUnavailable
	}

	var metadata accounts.TokenMetadata
	err := s.db.QueryRowContext(ctx, `SELECT user_id, token_hash, expires_at, token_type, email
		FROM `+userTokenTableName+` WHERE token_hash = ? AND token_type = ?`, tokenHash, purpose).
		Scan(&metadata.AccountID, &metadata.TokenHash, &metadata.ExpiresAt, &metadata.Purpose, &metadata.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.TokenMetadata{}, hs_errors.WithCause(accounts.ErrTokenNotFound, fmt.Errorf("get token by value"))
	}
	if err != nil {
		return accounts.TokenMetadata{}, hs_errors.WithCause(database.ErrDatabase, fmt.Errorf("get token by value: %w", err))
	}
	return metadata, nil
}

// Create inserts a new account. An existing email returns an error matching [accounts.ErrEmailAlreadyExists].
// The supplied user is unchanged if insertion fails.
func (s *AccountRepository) Create(ctx context.Context, user *accounts.User) error {
	if user == nil {
		return accounts.ErrUserNotSpecified
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
		return hs_errors.WithCause(database.ErrDatabaseUnavailable, fmt.Errorf("no database connection is specified"))
	}

	created := *user
	created.ID = uuid.New().String()
	created.CreatedAt = time.Now()
	created.UpdatedAt = created.CreatedAt
	created.SessionVersion = 1

	query := fmt.Sprintf(`
        INSERT INTO %s (id, email, email_key, verified, verification_required, created_at, updated_at, registration_auth_type, password, role, invited)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, userTableName)

	if _, err := s.db.ExecContext(ctx, query,
		created.ID, created.Email, accounts.EmailKey(created.Email), created.Verified, created.VerificationRequired,
		created.CreatedAt, created.UpdatedAt, created.RegistrationAuthType, created.Password, created.Role, created.Invited); err != nil {
		return accountWriteError(err)
	}
	*user = created
	return nil
}

// InsertToken inserts token metadata without replacing or revoking existing tokens.
// The account schema must already exist. The raw token is never passed to storage.
func (s *AccountRepository) InsertToken(ctx context.Context, metadata accounts.TokenMetadata) error {
	if s.db == nil {
		return database.ErrDatabaseUnavailable
	}
	errInsertToken := errors.New("error creating a new token in the database")
	if metadata.AccountID == "" {
		return hs_errors.WithCause(accounts.ErrUserNotSpecified, errInsertToken)
	}
	if metadata.TokenHash == "" {
		return hs_errors.WithCause(accounts.ErrTokenNotSpecified, errInsertToken)
	}
	if metadata.ExpiresAt.IsZero() {
		return hs_errors.WithCause(accounts.ErrTokenExpirationNotSpecified, errInsertToken)
	}
	if metadata.Purpose != accounts.ResetTokenPurpose && metadata.Purpose != accounts.VerificationTokenPurpose && metadata.Purpose != accounts.PasswordSetupTokenPurpose && metadata.Purpose != accounts.InvitationTokenPurpose {
		return hs_errors.WithCause(accounts.ErrToken, errors.New("invalid token purpose"))
	}

	_, err := s.db.ExecContext(ctx, `INSERT INTO `+userTokenTableName+`
		(user_id, token_hash, expires_at, token_type, email) VALUES (?, ?, ?, ?, ?)`,
		metadata.AccountID, metadata.TokenHash, metadata.ExpiresAt, metadata.Purpose, metadata.Email)
	return err
}

// Update changes an existing account by ID. It never inserts an account and
// returns [accounts.ErrUserNotFound] if the ID no longer exists. Changing the email address
// revokes stored verification tokens in the same transaction. Changes to credentials,
// email, provider, or verification settings also invalidate authenticated sessions.
// The supplied user must carry the security version from Create or an account read.
// Case-only email edits preserve verification tokens and the session version.
// If security state has since changed, Update returns [accounts.ErrUserChanged] without
// changing the account, its tokens, or the supplied user. Reload the account and
// reapply the intended edit before retrying. Do not copy only the new version.
func (s *AccountRepository) Update(ctx context.Context, user *accounts.User) error {
	if user == nil {
		return accounts.ErrUserNotSpecified
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
		return hs_errors.WithCause(database.ErrDatabaseUnavailable, fmt.Errorf("no database connection is specified"))
	}

	updated := *user
	updated.UpdatedAt = time.Now()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Revoke before updating so the comparison uses the stored email address.
	// Starting with a write also avoids upgrading a SQLite read transaction.
	_, err = tx.ExecContext(ctx, `DELETE FROM `+userTokenTableName+`
		WHERE user_id = ? AND token_type IN (?, ?)
		AND EXISTS (SELECT 1 FROM `+userTableName+` WHERE id = ? AND email_key <> ?)`,
		user.ID, accounts.VerificationTokenPurpose, accounts.InvitationTokenPurpose, user.ID, accounts.EmailKey(user.Email))
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
        UPDATE %s
        SET email = :email,
			email_key = :email_key,
            verified = :verified,
			verification_required = :verification_required,
            updated_at = :updated_at,
            registration_auth_type = :registration_auth_type,
            session_version = session_version + CASE WHEN password IS NOT :password
                OR email_key IS NOT :email_key OR verified IS NOT :verified
                OR verification_required IS NOT :verification_required
                OR registration_auth_type IS NOT :registration_auth_type
                OR role IS NOT :role OR invited IS NOT :invited THEN 1 ELSE 0 END,
            role = :role, invited = :invited,
            password = :password
        WHERE id = :id AND session_version = :session_version
    `, userTableName)

	result, err := tx.ExecContext(ctx, query,
		sql.Named("email", updated.Email),
		sql.Named("email_key", accounts.EmailKey(updated.Email)),
		sql.Named("verified", updated.Verified),
		sql.Named("verification_required", updated.VerificationRequired),
		sql.Named("updated_at", updated.UpdatedAt),
		sql.Named("registration_auth_type", updated.RegistrationAuthType),
		sql.Named("password", updated.Password),
		sql.Named("role", updated.Role),
		sql.Named("invited", updated.Invited),
		sql.Named("id", updated.ID),
		sql.Named("session_version", updated.SessionVersion))
	if err != nil {
		return accountWriteError(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}

	err = tx.QueryRowContext(ctx, `SELECT session_version FROM user WHERE id = ?`, user.ID).Scan(&updated.SessionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return hs_errors.WithCause(accounts.ErrUserNotFound, sql.ErrNoRows)
	}
	if err != nil {
		return err
	}
	if count == 0 {
		return accounts.ErrUserChanged
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	user.UpdatedAt = updated.UpdatedAt
	user.SessionVersion = updated.SessionVersion
	return nil
}

func accountWriteError(err error) error {
	var constraint sqlite3.Error
	// The email key is the account table's only UNIQUE constraint. Primary-key conflicts
	// and other constraint failures must remain storage errors.
	if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
		return fmt.Errorf("%w: %w", accounts.ErrEmailAlreadyExists, err)
	}
	return err
}

// ChangePassword stores a validated password hash and invalidates all existing
// authenticated sessions. A concurrent account change rejects the operation.
// The supplied user changes only after the database update succeeds.
func (s *AccountRepository) ChangePassword(ctx context.Context, u *accounts.User, passwordHash string) error {
	if u == nil || u.ID == "" {
		return accounts.ErrUserNotSpecified
	}
	if s.db == nil {
		return database.ErrDatabaseUnavailable
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
// transaction. The caller must read authorization metadata by token hash.
func (s *AccountRepository) ResetPassword(ctx context.Context, u *accounts.User, authorization accounts.ResetAuthorization, passwordHash string) error {
	return s.replacePassword(ctx, u, accounts.TokenMetadata{AccountID: authorization.AccountID, TokenHash: authorization.TokenHash, ExpiresAt: authorization.ExpiresAt, Purpose: accounts.ResetTokenPurpose}, passwordHash)
}

// SetupPassword enrolls a password only while the authenticated account snapshot
// is current. Setup tokens cannot be consumed by ResetPassword.
func (s *AccountRepository) SetupPassword(ctx context.Context, u *accounts.User, token accounts.TokenMetadata, passwordHash string) error {
	if token.Purpose != accounts.PasswordSetupTokenPurpose || accounts.EmailKey(token.Email) == "" {
		return accounts.ErrToken
	}
	if u == nil || u.Password != "" || u.NeedsVerification() {
		return accounts.ErrUserChanged
	}
	if accounts.EmailKey(token.Email) != accounts.EmailKey(u.Email) {
		return accounts.ErrToken
	}
	return s.replacePassword(ctx, u, token, passwordHash)
}

func (s *AccountRepository) replacePassword(ctx context.Context, u *accounts.User, authorization accounts.TokenMetadata, passwordHash string) error {
	if u == nil || u.ID == "" {
		return accounts.ErrUserNotSpecified
	}
	if s.db == nil {
		return database.ErrDatabaseUnavailable
	}
	if passwordHash == "" {
		return errors.New("a password hash is required")
	}
	if authorization.AccountID != u.ID || authorization.TokenHash == "" || authorization.ExpiresAt.IsZero() {
		return hs_errors.WithCause(accounts.ErrToken, errors.New("invalid reset authorization"))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	recipient, err := deleteTokenAndCheckExpiry(ctx, tx, authorization.AccountID, authorization.TokenHash, authorization.Purpose, u.Email, authorization.ExpiresAt)
	if err != nil {
		return err
	}
	now := time.Now()

	// Only update the password. Do not overwrite unrelated account changes with
	// the snapshot used for form validation or the previous-password check.
	updated, err := scanAccount(tx.QueryRowContext(ctx, `UPDATE `+userTableName+` SET password = ?, updated_at = ?, session_version = session_version + 1
		WHERE id = ? AND password = ? AND registration_auth_type = ? AND (? = '' OR email_key = ?)
		AND (? = 0 OR (password = '' AND session_version = ? AND (verified = 1 OR verification_required = 0)))
		RETURNING `+accountColumns,
		passwordHash, now, u.ID, u.Password, u.RegistrationAuthType, recipient, accounts.EmailKey(recipient), authorization.Purpose == accounts.PasswordSetupTokenPurpose, u.SessionVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return hs_errors.WithCause(accounts.ErrToken, errors.New("account changed or no longer exists"))
	}
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	*u = *updated
	return nil
}

// Verify consumes a validated verification token and verifies its email address
// in one transaction. The caller must read authorization metadata by token hash.
func (s *AccountRepository) Verify(ctx context.Context, authorization accounts.VerificationAuthorization) (*accounts.User, error) {
	if s.db == nil {
		return nil, database.ErrDatabaseUnavailable
	}
	if authorization.AccountID == "" || authorization.Email == "" || authorization.TokenHash == "" || authorization.ExpiresAt.IsZero() {
		return nil, hs_errors.WithCause(accounts.ErrToken, errors.New("invalid verification authorization"))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := deleteTokenAndCheckExpiry(ctx, tx, authorization.AccountID, authorization.TokenHash, accounts.VerificationTokenPurpose, authorization.Email, authorization.ExpiresAt); err != nil {
		return nil, err
	}

	account, err := scanAccount(tx.QueryRowContext(ctx, `UPDATE `+userTableName+`
		SET session_version = session_version + CASE WHEN verified AND NOT invited THEN 0 ELSE 1 END,
		invited = CASE WHEN registration_auth_type<>'' THEN FALSE ELSE invited END,
		verified = TRUE, updated_at = ? WHERE id = ? AND email_key = ?
		RETURNING `+accountColumns,
		time.Now(), authorization.AccountID, accounts.EmailKey(authorization.Email)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, hs_errors.WithCause(accounts.ErrToken, errors.New("verification address changed or account no longer exists"))
	}
	if err != nil {
		return nil, err
	}

	if !account.Invited {
		if _, err := tx.ExecContext(ctx, "DELETE FROM usertoken WHERE user_id=? AND token_type=?", account.ID, accounts.InvitationTokenPurpose); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return account, nil
}

// deleteTokenAndCheckExpiry deletes the matching token, then checks its stored
// and supplied expiry after acquiring the write lock. The caller must roll back on
// any error to restore the token.
func deleteTokenAndCheckExpiry(ctx context.Context, tx *sql.Tx, accountID, tokenHash, purpose, email string, suppliedExpiry time.Time) (string, error) {
	var expiresAt time.Time
	var storedEmail string
	err := tx.QueryRowContext(ctx, `DELETE FROM `+userTokenTableName+`
		WHERE user_id = ? AND token_hash = ? AND token_type = ?
		RETURNING expires_at, email`, accountID, tokenHash, purpose).
		Scan(&expiresAt, &storedEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return "", hs_errors.WithCause(accounts.ErrTokenNotFound, err)
	}
	if err != nil {
		return "", err
	}

	now := time.Now()
	if !now.Before(expiresAt) || !now.Before(suppliedExpiry) {
		return "", accounts.ErrTokenExpired
	}
	if purpose != accounts.ResetTokenPurpose && (accounts.EmailKey(storedEmail) == "" || accounts.EmailKey(storedEmail) != accounts.EmailKey(email)) {
		return "", accounts.ErrToken
	}
	if purpose == accounts.ResetTokenPurpose {
		return "", nil
	}
	return storedEmail, nil
}
