package user

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/database"
)

// SQLiteAccountRepository stores accounts using an existing SQLite pool.
// Its owner must prepare the account schema and close the pool after consumers stop.
type SQLiteAccountRepository struct{ db *sqlx.DB }

// NewSQLiteAccountRepository borrows db. It does not open, initialize, or close storage.
func NewSQLiteAccountRepository(db *sqlx.DB) *SQLiteAccountRepository {
	return &SQLiteAccountRepository{db: db}
}

var _ AccountRepository = (*SQLiteAccountRepository)(nil)

// GetByID loads an account by ID, or returns ErrUserNotFound.
func (s *SQLiteAccountRepository) GetByID(ctx context.Context, id string) (*User, error) {
	u, err := GetUserByIDContext(ctx, s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewErrUserNotFound(err)
	}
	return u, err
}

// GetByEmail loads an account by email, or returns ErrUserNotFound.
func (s *SQLiteAccountRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	u, err := GetUserByEmailContext(ctx, s.db, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewErrUserNotFound(err)
	}
	return u, err
}

// Create inserts an account without changing an existing account with the same email.
func (s *SQLiteAccountRepository) Create(ctx context.Context, u *User) error {
	return u.Create(ctx, s.db)
}

// Update stores account changes and their token revocations in one transaction.
func (s *SQLiteAccountRepository) Update(ctx context.Context, u *User) error {
	return u.Update(ctx, s.db)
}

// ChangePassword conditionally stores a password hash and revokes existing sessions.
func (s *SQLiteAccountRepository) ChangePassword(ctx context.Context, u *User, passwordHash string) error {
	return u.ChangePassword(ctx, s.db, passwordHash)
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
