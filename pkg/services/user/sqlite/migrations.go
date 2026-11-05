package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var accountMigrations embed.FS

// bootstrapAccountSchema applies the numbered SQL files only to empty storage.
// The caller owns the transaction, including validation and rollback. Existing
// databases must be migrated explicitly with the CLI, never during startup.
func bootstrapAccountSchema(ctx context.Context, tx *sql.Tx, source fs.FS, requiredVersion int) error {
	files, err := fs.Glob(source, "migrations/*.up.sql")
	if err != nil {
		return fmt.Errorf("list account migrations: %w", err)
	}
	if requiredVersion < 1 || len(files) != requiredVersion {
		return fmt.Errorf("account migrations must contain versions 1 through %d", requiredVersion)
	}
	if _, err := tx.ExecContext(ctx, accountVersionSchema); err != nil {
		return fmt.Errorf("create account schema version table: %w", err)
	}
	for i, name := range files {
		prefix, _, _ := strings.Cut(path.Base(name), "_")
		version, err := strconv.Atoi(prefix)
		if err != nil || version != i+1 {
			return fmt.Errorf("account migration %q must have version %d", name, i+1)
		}
		script, err := fs.ReadFile(source, name)
		if err != nil {
			return fmt.Errorf("read account migration %q: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(script)); err != nil {
			return fmt.Errorf("apply account migration %q: %w", name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO hyperserver_account_version (version, dirty) VALUES (?, false)`, requiredVersion); err != nil {
		return fmt.Errorf("record account schema version %d: %w", requiredVersion, err)
	}
	return nil
}
