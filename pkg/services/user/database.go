package user

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
)

const (
	userTableName      = "user"
	userTokenTableName = "usertoken"
)

// PrepareDatabase creates missing account and token tables in a SQLite database.
// Applications must call it before using account storage. It is safe to call
// again. Existing accounts receive a session version without changing their credentials.
func PrepareDatabase(ctx context.Context, db *sqlx.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}
	if _, ok := db.Driver().(*sqlite3.SQLiteDriver); !ok {
		return database.NewErrDatabaseNotSupported(fmt.Errorf("the database type %T is not supported", db.Driver()))
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin account schema setup: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS user (
		id VARCHAR(36) PRIMARY KEY,
		email VARCHAR(255) NOT NULL UNIQUE,
		verified BOOLEAN NOT NULL DEFAULT FALSE,
		verification_required BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		registration_auth_type VARCHAR(50),
		password VARCHAR(255)
	)`)
	if err != nil {
		return fmt.Errorf("create account table: %w", err)
	}
	var versionColumn int
	if err := tx.GetContext(ctx, &versionColumn, `SELECT count(*) FROM pragma_table_info('user') WHERE name = 'session_version'`); err != nil {
		return fmt.Errorf("check account session version: %w", err)
	}
	if versionColumn == 0 {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE user ADD COLUMN session_version INTEGER NOT NULL DEFAULT 1`); err != nil {
			return fmt.Errorf("add account session version: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS usertoken (
		user_id VARCHAR(36),
		token_hash VARCHAR(255) NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		token_type VARCHAR(255) NOT NULL,
		PRIMARY KEY (user_id, token_hash),
		FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE
	)`)
	if err != nil {
		return fmt.Errorf("create account token table: %w", err)
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS usertoken_expiration
		ON usertoken (julianday(expires_at), user_id, token_hash)`)
	if err != nil {
		return fmt.Errorf("create account token expiration index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account schema setup: %w", err)
	}
	return nil
}
