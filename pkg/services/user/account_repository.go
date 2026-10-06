package user

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// AccountRepository loads and creates accounts for authentication. Missing accounts
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
}

// SQLiteAccountRepository reads and creates accounts using an existing SQLite pool.
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
