package user

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// AccountReader loads accounts for authentication. Missing accounts must return
// ErrUserNotFound. Other failures must remain errors. Implementations must honor
// cancellation and support concurrent reads. Successful reads must return
// independent, non-nil User values.
type AccountReader interface {
	GetByID(context.Context, string) (*User, error)
	GetByEmail(context.Context, string) (*User, error)
}

// SQLiteAccountReader reads accounts using an existing SQLite connection pool.
// Its owner must prepare the account schema and close the pool after consumers stop.
type SQLiteAccountReader struct{ db *sqlx.DB }

// NewSQLiteAccountReader borrows db. It does not open, initialize, or close storage.
func NewSQLiteAccountReader(db *sqlx.DB) *SQLiteAccountReader {
	return &SQLiteAccountReader{db: db}
}

var _ AccountReader = (*SQLiteAccountReader)(nil)

// GetByID loads an account by ID, or returns ErrUserNotFound.
func (s *SQLiteAccountReader) GetByID(ctx context.Context, id string) (*User, error) {
	u, err := GetUserByIDContext(ctx, s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewErrUserNotFound(err)
	}
	return u, err
}

// GetByEmail loads an account by email, or returns ErrUserNotFound.
func (s *SQLiteAccountReader) GetByEmail(ctx context.Context, email string) (*User, error) {
	u, err := GetUserByEmailContext(ctx, s.db, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewErrUserNotFound(err)
	}
	return u, err
}
