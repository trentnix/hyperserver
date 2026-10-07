package user

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// AccountRepository stores accounts for authentication. Reads of missing accounts
// must return ErrUserNotFound. Other failures must remain errors. Implementations
// must honor cancellation and support concurrent calls. Successful reads must
// return independent, non-nil User values.
type AccountRepository interface {
	GetByID(context.Context, string) (*User, error)
	GetByEmail(context.Context, string) (*User, error)
	// Create inserts only. Duplicate emails return database.ErrRecordAlreadyExists.
	// Failure must leave stored accounts and u unchanged. Success fills in the ID,
	// timestamps, and initial SessionVersion of 1. Password is already hashed.
	Create(context.Context, *User) error
	// Update changes an existing account by ID. Missing accounts return ErrUserNotFound,
	// stale SessionVersion values return ErrUserChanged, and duplicate emails return
	// database.ErrRecordAlreadyExists. The caller must preserve the version from its
	// account read and validate and hash any new password before calling.
	// Changes to Password, Email, RegistrationAuthType, Verified, or VerificationRequired
	// must increment SessionVersion. Otherwise, the version must stay unchanged.
	// ID and CreatedAt must stay unchanged. An email change must also revoke the account's
	// verification tokens. These changes must commit together and preserve other tokens.
	// Success updates u's UpdatedAt and SessionVersion. Failure must leave u and storage unchanged.
	Update(context.Context, *User) error
	// ChangePassword atomically replaces the password hash and increments SessionVersion
	// only if the stored password, registration provider, and SessionVersion match u.
	// A missing account or a mismatch must return an error. Failure must leave storage
	// and u unchanged. Success updates u's Password, UpdatedAt, and SessionVersion.
	// The caller must validate and hash the new password before calling.
	ChangePassword(ctx context.Context, u *User, passwordHash string) error
}

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
