// Package database prepares the development site's SQLite schema.
package database

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
)

//go:embed schema.sql
var schema string

// PrepareDatabase creates missing tables and checks the contact column declarations
// and unique ID key without changing existing data.
// The site owns its table names and schema. The caller owns the borrowed pool.
// This prepares the current schema only. It does not apply versioned migrations.
func PrepareDatabase(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return database.NewErrDatabaseUnavailable(fmt.Errorf("the database is not valid"))
	}

	if _, ok := db.Driver().(*sqlite3.SQLiteDriver); !ok {
		return database.NewErrDatabaseNotSupported(fmt.Errorf("the database type %T is not supported", db.Driver()))
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin site schema setup: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("prepare site schema: %w", err)
	}
	if err := validateContactSchema(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit site schema setup: %w", err)
	}
	return nil
}

func validateContactSchema(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []string{
		"id",
		"email",
		"name",
		"message",
		"created_at",
	} {
		var declaredType string
		var notNull bool
		var hidden int
		err := tx.QueryRowContext(ctx, `SELECT type, "notnull", hidden
			FROM pragma_table_xinfo('hyperserver_contact_submission') WHERE name = ? COLLATE NOCASE`, column).
			Scan(&declaredType, &notNull, &hidden)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check SQLite contact schema columns: missing %s", column)
		}
		if err != nil {
			return fmt.Errorf("check SQLite contact schema columns: %w", err)
		}
		if hidden != 0 {
			return fmt.Errorf("incompatible SQLite contact schema: %s must be an ordinary writable column", column)
		}

		// Text affinity preserves strings. Timestamp declarations must also be
		// recognized by the driver when records are read into time.Time.
		typeName := strings.ToUpper(declaredType)
		kind := "text"
		compatible := !strings.Contains(typeName, "INT") && (strings.Contains(typeName, "CHAR") || strings.Contains(typeName, "CLOB") || strings.Contains(typeName, "TEXT"))
		if column == "created_at" {
			kind = "timestamp"
			compatible = typeName == "DATE" || typeName == "DATETIME" || typeName == "TIMESTAMP"
		}
		if !compatible {
			return fmt.Errorf("incompatible SQLite contact schema: %s requires a supported %s type, got %q", column, kind, declaredType)
		}
		if column == "email" && !notNull {
			return fmt.Errorf("incompatible SQLite contact schema: email requires NOT NULL")
		}
	}

	// A composite, partial, or expression index does not establish ID uniqueness.
	var uniqueID bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM pragma_index_list('hyperserver_contact_submission') AS indexes
		JOIN pragma_index_xinfo(indexes.name) AS columns
		WHERE indexes."unique" = 1 AND indexes.partial = 0 AND columns."key" = 1
		GROUP BY indexes.name
		HAVING count(*) = 1 AND columns.name = 'id' COLLATE NOCASE AND columns.coll = 'BINARY' COLLATE NOCASE
	)`).Scan(&uniqueID)
	if err != nil {
		return fmt.Errorf("check SQLite contact unique key: %w", err)
	}
	if !uniqueID {
		return fmt.Errorf("incompatible SQLite contact schema: id requires a non-partial unique key with BINARY comparison")
	}
	return nil
}
