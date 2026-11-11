package backup

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/trentnix/hyperserver/modules/blog"
)

// Recovery supports these stored formats, not arbitrary SQLite files. Keep this
// policy explicit when adding migrations. Verification never upgrades a backup.
func checkCompatibility(ctx context.Context, db *sql.DB, roles []string) error {
	for _, role := range roles {
		switch role {
		case "blog":
			if err := blog.ValidateSchema(ctx, db); err != nil {
				return fmt.Errorf("incompatible blog backup: %w", err)
			}
		case "accounts":
			if err := checkSchemaVersion(ctx, db, "hyperserver_account_version", role, 1); err != nil {
				return err
			}
		case "sessions":
			// Custom session table names have their own version tables. Inspect
			// the snapshot, never load or follow paths in archived configuration.
			const prefix = "hyperserver_session_version_"
			rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND substr(lower(name), 1, ?)=? ORDER BY name`, len(prefix), prefix)
			if err != nil {
				return err
			}
			var tables []string
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					rows.Close()
					return err
				}
				tables = append(tables, name)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if len(tables) == 0 {
				return fmt.Errorf("incompatible sessions backup: missing schema version")
			}
			for _, table := range tables {
				if err := checkSchemaVersion(ctx, db, table, role, 1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkSchemaVersion(ctx context.Context, db *sql.DB, table, role string, required int64) error {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=? COLLATE NOCASE)`, table).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("incompatible %s backup: missing schema version", role)
	}
	var count int
	var version sql.NullInt64
	var dirty sql.NullBool
	name := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
	err := db.QueryRowContext(ctx, "SELECT count(*), min(version), min(dirty) FROM "+name).Scan(&count, &version, &dirty)
	if err != nil {
		return fmt.Errorf("incompatible %s backup: read schema version: %w", role, err)
	}
	if count != 1 || !version.Valid || !dirty.Valid {
		return fmt.Errorf("incompatible %s backup: invalid schema version metadata", role)
	}
	if dirty.Bool {
		return fmt.Errorf("incompatible %s backup: schema version %d is dirty", role, version.Int64)
	}
	if version.Int64 != required {
		return fmt.Errorf("incompatible %s backup: schema version %d, supported version %d; use a compatible maintenance binary", role, version.Int64, required)
	}
	return nil
}
