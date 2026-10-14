package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
)

const (
	userTableName      = "user"
	userTokenTableName = "usertoken"
)

// PrepareDatabase creates missing account and token tables and checks the columns
// required by the repository. Applications must call it before using account
// storage. Repeated calls preserve data. It does not migrate existing tables.
func PrepareDatabase(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("no database connection is specified"))
	}
	if _, ok := db.Driver().(*sqlite3.SQLiteDriver); !ok {
		return database.NewErrDatabaseNotSupported(fmt.Errorf("the database type %T is not supported", db.Driver()))
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin account schema setup: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS user (
		id VARCHAR(36) PRIMARY KEY,
		email VARCHAR(255) NOT NULL,
		email_key TEXT NOT NULL UNIQUE,
		verified BOOLEAN NOT NULL DEFAULT FALSE,
		verification_required BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		registration_auth_type VARCHAR(50),
		password VARCHAR(255),
		session_version INTEGER NOT NULL DEFAULT 1
	)`)
	if err != nil {
		return fmt.Errorf("create account table: %w", err)
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
	if err := validateAccountColumns(ctx, tx); err != nil {
		return err
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

// validateAccountColumns compiles read queries without loading account data.
// Column presence is only one compatibility check, not a stored-format version
// or a complete check of constraints and existing values.
func validateAccountColumns(ctx context.Context, tx *sql.Tx) error {
	for _, query := range []string{
		"SELECT " + accountColumns + ", email_key FROM user LIMIT 0",
		"SELECT user_id, token_hash, expires_at, token_type FROM usertoken LIMIT 0",
	} {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("check SQLite account schema columns: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close account schema check: %w", err)
		}
	}
	return nil
}
