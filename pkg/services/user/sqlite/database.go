package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
)

const (
	userTableName      = "user"
	userTokenTableName = "usertoken"
)

// PrepareDatabase creates missing account and token tables and checks required
// columns and unique keys. Applications must call it before using account storage.
// Repeated calls preserve data. It does not migrate existing tables.
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
	for _, key := range []struct {
		table   string
		columns []string
	}{
		{"user", []string{"id"}},
		{"user", []string{"email_key"}},
		{"usertoken", []string{"user_id", "token_hash"}},
	} {
		if err := requireAccountUniqueKey(ctx, tx, key.table, key.columns); err != nil {
			return err
		}
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
// Column presence does not establish compatible types or stored values.
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

// requireAccountUniqueKey accepts a primary key, UNIQUE constraint, or explicit
// unique index with exactly these columns and binary comparison. Partial and
// expression indexes do not establish the repository's required uniqueness.
// Index column order and index names do not matter.
func requireAccountUniqueKey(ctx context.Context, tx *sql.Tx, table string, columns []string) error {
	rows, err := tx.QueryContext(ctx, `SELECT indexes.name, columns.name, columns.coll
		FROM pragma_index_list(?) AS indexes
		JOIN pragma_index_xinfo(indexes.name) AS columns
		WHERE indexes."unique" = 1 AND indexes.partial = 0 AND columns."key" = 1`, table)
	if err != nil {
		return fmt.Errorf("check SQLite account unique keys on %s: %w", table, err)
	}
	defer rows.Close()

	keys := make(map[string][]string)
	for rows.Next() {
		var index string
		var column, collation sql.NullString
		if err := rows.Scan(&index, &column, &collation); err != nil {
			return fmt.Errorf("read SQLite account unique key on %s: %w", table, err)
		}
		name := strings.ToLower(column.String)
		if !column.Valid || !strings.EqualFold(collation.String, "BINARY") {
			name = "" // Never matches a required column.
		}
		keys[index] = append(keys[index], name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read SQLite account unique keys on %s: %w", table, err)
	}

	expected := slices.Clone(columns)
	slices.Sort(expected)
	for _, key := range keys {
		slices.Sort(key)
		if slices.Equal(key, expected) {
			return nil
		}
	}
	return fmt.Errorf("incompatible SQLite account schema: %s requires a non-partial unique key on (%s) with BINARY comparison", table, strings.Join(columns, ", "))
}
