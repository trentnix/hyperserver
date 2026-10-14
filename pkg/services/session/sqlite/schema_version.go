package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed migrations/000001_sessions.up.sql
var sessionSchemaV1 string

//go:embed migrations/000001_sessions.down.sql
var sessionSchemaDownV1 string

func sessionVersionTable(table string) string {
	return "hyperserver_session_version_" + table
}

// Replace only complete quoted identifiers, never SQL fragments supplied by a
// caller. NewReplacer does not reprocess text inserted by a replacement.
func sessionMigrationSQL(table string) (up, down string) {
	replace := strings.NewReplacer(
		`"sessions"`, quoteIdentifier(table),
		`"sessions_expiration"`, quoteIdentifier(table+"_expiration"),
	)
	return replace.Replace(sessionSchemaV1), replace.Replace(sessionSchemaDownV1)
}

func prepareSessionVersion(ctx context.Context, tx *sql.Tx, table string) error {
	versionTable := sessionVersionTable(table)
	var objects int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name COLLATE NOCASE IN (?, ?, ?)`,
		table, table+"_expiration", versionTable).Scan(&objects)
	if err != nil {
		return fmt.Errorf("inspect SQLite session storage %q: %w", table, err)
	}
	if objects == 0 {
		up, _ := sessionMigrationSQL(table)
		if _, err := tx.ExecContext(ctx, up); err != nil {
			return fmt.Errorf("create SQLite session schema %q version 1: %w", table, err)
		}
		if _, err := tx.ExecContext(ctx, "CREATE TABLE "+quoteIdentifier(versionTable)+" (version uint64, dirty bool)"); err != nil {
			return fmt.Errorf("create SQLite session version table %q: %w", table, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+quoteIdentifier(versionTable)+" VALUES (1, false)"); err != nil {
			return fmt.Errorf("record SQLite session version %q: %w", table, err)
		}
		return nil
	}
	var hasVersion bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM sqlite_master
		WHERE type = 'table' AND name = ? COLLATE NOCASE)`, versionTable).Scan(&hasVersion)
	if err != nil {
		return fmt.Errorf("inspect SQLite session schema version %q: %w", table, err)
	}
	if !hasVersion {
		return fmt.Errorf("unversioned SQLite session storage %q: no upgrade path is supported", table)
	}
	var count int
	var version sql.NullInt64
	var dirty sql.NullBool
	err = tx.QueryRowContext(ctx, "SELECT count(*), min(version), min(dirty) FROM "+quoteIdentifier(versionTable)).Scan(&count, &version, &dirty)
	if err != nil {
		return fmt.Errorf("read SQLite session schema version %q: %w", table, err)
	}
	if count != 1 || !version.Valid || !dirty.Valid {
		return fmt.Errorf("invalid SQLite session schema version %q: expected one version and dirty flag", table)
	}
	if dirty.Bool {
		return fmt.Errorf("SQLite session schema %q version %d is dirty: explicit migration repair is required", table, version.Int64)
	}
	if version.Int64 != 1 {
		return fmt.Errorf("unsupported SQLite session schema %q version %d: this provider supports version 1", table, version.Int64)
	}
	return nil
}
