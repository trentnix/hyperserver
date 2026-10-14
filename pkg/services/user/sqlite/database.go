package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/pkg/database"
)

const (
	userTableName       = "user"
	userTokenTableName  = "usertoken"
	accountVersionTable = "hyperserver_account_version"
)

// accountVersionSchema matches golang-migrate's SQLite version/dirty format.
// Keep this compatibility boundary in the provider, not the core server.
const accountVersionSchema = `CREATE TABLE hyperserver_account_version (version uint64, dirty bool)`

//go:embed migrations/000001_accounts.up.sql
var accountSchemaV1 string

// PrepareDatabase creates version 1 in fresh account storage and checks required
// columns, supported types, nullability, unique keys, and the token foreign key.
// Applications must call it before using account storage. Repeated calls preserve
// data. Existing storage must already have supported version metadata. It does
// not adopt or migrate existing tables or validate every stored value.
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

	if err := prepareAccountVersion(ctx, tx); err != nil {
		return err
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
	if err := requireTokenForeignKey(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account schema setup: %w", err)
	}
	return nil
}

// prepareAccountVersion bootstraps only empty account storage. Existing stores
// must have a clean, supported golang-migrate version before startup.
func prepareAccountVersion(ctx context.Context, tx *sql.Tx) error {
	var objects int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master
		WHERE name COLLATE NOCASE IN ('user', 'usertoken', 'usertoken_expiration', ?)`, accountVersionTable).Scan(&objects)
	if err != nil {
		return fmt.Errorf("inspect account storage: %w", err)
	}
	if objects == 0 {
		if _, err := tx.ExecContext(ctx, accountVersionSchema); err != nil {
			return fmt.Errorf("create account schema version table: %w", err)
		}
		if _, err := tx.ExecContext(ctx, accountSchemaV1); err != nil {
			return fmt.Errorf("create account schema version 1: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO hyperserver_account_version (version, dirty) VALUES (1, false)`); err != nil {
			return fmt.Errorf("record account schema version 1: %w", err)
		}
		return nil
	}

	var hasHistory bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM sqlite_master
		WHERE type = 'table' AND name = ? COLLATE NOCASE)`, accountVersionTable).Scan(&hasHistory)
	if err != nil {
		return fmt.Errorf("inspect account schema version: %w", err)
	}
	if !hasHistory {
		return fmt.Errorf("unversioned SQLite account storage: explicit baseline adoption is required before startup")
	}
	var count int
	var version sql.NullInt64
	var dirty sql.NullBool
	err = tx.QueryRowContext(ctx, `SELECT count(*), min(version), min(dirty) FROM hyperserver_account_version`).Scan(&count, &version, &dirty)
	if err != nil {
		return fmt.Errorf("read account schema version: %w", err)
	}
	if count != 1 || !version.Valid || !dirty.Valid {
		return fmt.Errorf("invalid SQLite account schema version: expected one version and dirty flag")
	}
	if dirty.Bool {
		return fmt.Errorf("SQLite account schema version %d is dirty: explicit migration repair is required before startup", version.Int64)
	}
	if version.Int64 != 1 {
		return fmt.Errorf("unsupported SQLite account schema version %d: this provider supports version 1", version.Int64)
	}
	return nil
}

// requireTokenForeignKey checks the single-column relationship that removes
// tokens when their account is deleted. An omitted parent column is valid only
// when the account's entire primary key is id.
func requireTokenForeignKey(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT "table", "from", "to", on_delete
		FROM pragma_foreign_key_list('usertoken')
		GROUP BY id HAVING count(*) = 1`)
	if err != nil {
		return fmt.Errorf("check SQLite account token foreign key: %w", err)
	}
	defer rows.Close()

	implicitParent := false
	for rows.Next() {
		var table, from, onDelete string
		var to sql.NullString
		if err := rows.Scan(&table, &from, &to, &onDelete); err != nil {
			return fmt.Errorf("read SQLite account token foreign key: %w", err)
		}
		if !strings.EqualFold(table, "user") || !strings.EqualFold(from, "user_id") || onDelete != "CASCADE" {
			continue
		}
		if to.Valid && strings.EqualFold(to.String, "id") {
			return nil
		}
		if !to.Valid {
			implicitParent = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read SQLite account token foreign keys: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close SQLite account token foreign key check: %w", err)
	}

	if implicitParent {
		var matches bool
		err := tx.QueryRowContext(ctx, `SELECT count(*) = 1 AND min(name) = 'id' COLLATE NOCASE
			FROM pragma_table_info('user') WHERE pk > 0`).Scan(&matches)
		if err != nil {
			return fmt.Errorf("check SQLite account primary key: %w", err)
		}
		if matches {
			return nil
		}
	}
	return fmt.Errorf("incompatible SQLite account schema: usertoken.user_id requires a foreign key to user(id) with ON DELETE CASCADE")
}

// validateAccountColumns checks declarations without reading account data.
// Historically nullable columns remain supported. Required columns must be
// writable because repository operations supply their values explicitly.
func validateAccountColumns(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []struct {
		table, name, kind string
		notNull           bool
	}{
		{"user", "id", "text", false},
		{"user", "email", "text", true},
		{"user", "email_key", "text", true},
		{"user", "verified", "boolean", true},
		{"user", "verification_required", "boolean", true},
		{"user", "created_at", "timestamp", true},
		{"user", "updated_at", "timestamp", true},
		{"user", "registration_auth_type", "text", false},
		{"user", "password", "text", false},
		{"user", "session_version", "integer", true},
		{"usertoken", "user_id", "text", false},
		{"usertoken", "token_hash", "text", true},
		{"usertoken", "expires_at", "timestamp", true},
		{"usertoken", "token_type", "text", true},
	} {
		var declaredType string
		var notNull bool
		var hidden int
		err := tx.QueryRowContext(ctx, `SELECT type, "notnull", hidden
			FROM pragma_table_xinfo(?) WHERE name = ? COLLATE NOCASE`, column.table, column.name).
			Scan(&declaredType, &notNull, &hidden)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check SQLite account schema columns: missing %s.%s", column.table, column.name)
		}
		if err != nil {
			return fmt.Errorf("check SQLite account schema columns: %w", err)
		}
		if hidden != 0 {
			return fmt.Errorf("incompatible SQLite account schema: %s.%s must be an ordinary writable column", column.table, column.name)
		}
		if !compatibleAccountColumnType(declaredType, column.kind) {
			return fmt.Errorf("incompatible SQLite account schema: %s.%s requires a supported %s type, got %q", column.table, column.name, column.kind, declaredType)
		}
		if column.notNull && !notNull {
			return fmt.Errorf("incompatible SQLite account schema: %s.%s requires NOT NULL", column.table, column.name)
		}
	}
	return nil
}

// compatibleAccountColumnType follows SQLite's text and integer affinity rules.
// Timestamps require a declaration the driver decodes into time.Time. Boolean
// columns support BOOL, BOOLEAN, and integer-affinity declarations.
func compatibleAccountColumnType(declaredType, kind string) bool {
	name := strings.ToUpper(declaredType)
	integer := strings.Contains(name, "INT")
	switch kind {
	case "text":
		return !integer && (strings.Contains(name, "CHAR") || strings.Contains(name, "CLOB") || strings.Contains(name, "TEXT"))
	case "integer":
		return integer
	case "boolean":
		return integer || name == "BOOL" || name == "BOOLEAN"
	case "timestamp":
		return name == "DATE" || name == "DATETIME" || name == "TIMESTAMP"
	default:
		return false
	}
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
