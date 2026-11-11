package blog

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"

	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

// ValidateSchema checks an existing blog database without initializing or upgrading it.
// The caller retains ownership of db. Validation uses the same checks as startup.
func ValidateSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := validateBlogSchema(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// validateBlogSchema checks version metadata and structure against a fresh bootstrap.
// SQLite supplies the metadata, so formatting and column order do not matter.
// The migration version and tests cover view and trigger behavior, CHECK
// expressions, and deferred constraints, which PRAGMAs do not describe.
func validateBlogSchema(ctx context.Context, tx *sql.Tx) error {
	if err := checkBlogVersion(ctx, tx); err != nil {
		return err
	}
	expected, err := dbsqlite.Open(":memory:")
	if err != nil {
		return err
	}
	defer expected.Close()
	if _, err := expected.ExecContext(ctx, blogSchema); err != nil {
		return fmt.Errorf("read required blog schema: %w", err)
	}
	objects, err := blogSchemaRows(ctx, expected, `SELECT type, name, tbl_name FROM sqlite_master WHERE type IN ('table','view','trigger') AND name GLOB 'blog_*' ORDER BY type, name`)
	if err != nil {
		return err
	}
	tables := make(map[string]bool)
	for _, object := range objects {
		kind, name := object[0].(string), object[1].(string)
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type=? AND name=? AND tbl_name=?)`, kind, name, object[2]).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("incompatible blog schema: missing %s %s", kind, name)
		}
		if kind == "trigger" {
			continue
		}
		const columns = `SELECT name, upper(type), "notnull", dflt_value, pk, hidden FROM pragma_table_xinfo(?) ORDER BY name`
		if err := compareBlogSchemaRows(ctx, tx, expected, columns, name); err != nil {
			return fmt.Errorf("incompatible blog schema: %s %s columns: %w", kind, name, err)
		}
		if kind == "view" {
			continue
		}
		tables[name] = true
		const references = `SELECT seq, "table", "from", "to", on_update, on_delete, match FROM pragma_foreign_key_list(?) ORDER BY "table", "from", "to", seq`
		if err := compareBlogSchemaRows(ctx, tx, expected, references, name); err != nil {
			return fmt.Errorf("incompatible blog schema: table %s references: %w", name, err)
		}
		if err := validateBlogIndexes(ctx, tx, expected, name); err != nil {
			return err
		}
	}
	actual, err := blogSchemaRows(ctx, tx, `SELECT name FROM sqlite_master WHERE type='table' AND lower(name) GLOB 'blog_*' AND lower(name)<>'blog_schema_version'`)
	if err != nil {
		return err
	}
	for _, row := range actual {
		if name := row[0].(string); !tables[name] {
			return fmt.Errorf("incompatible blog schema: unsupported table %s", name)
		}
	}
	return nil
}

// Required named indexes keep their names. SQLite-generated primary and unique
// indexes match by structure because rebuilding a table can change their names.
// Additional operator indexes are allowed.
func validateBlogIndexes(ctx context.Context, tx *sql.Tx, expected *sql.DB, table string) error {
	const indexes = `SELECT name, "unique", origin, partial FROM pragma_index_list(?)`
	want, err := blogSchemaRows(ctx, expected, indexes, table)
	if err != nil {
		return err
	}
	got, err := blogSchemaRows(ctx, tx, indexes, table)
	if err != nil {
		return err
	}
	// Extra implicit keys can change INSERT behavior. In particular, an
	// INTEGER PRIMARY KEY backed by an index is not a generated rowid alias.
	implicit := 0
	for _, index := range want {
		if index[2] != "c" {
			implicit++
		}
	}
	for _, index := range got {
		if index[2] != "c" {
			implicit--
		}
	}
	if implicit != 0 {
		return fmt.Errorf("incompatible blog schema: table %s primary or unique keys differ", table)
	}
	const keys = `SELECT name, coll, "desc" FROM pragma_index_xinfo(?) WHERE key=1 ORDER BY seqno`
	for _, index := range want {
		columns, err := blogSchemaRows(ctx, expected, keys, index[0])
		if err != nil {
			return err
		}
		found := false
		for _, candidate := range got {
			if !reflect.DeepEqual(index[1:], candidate[1:]) || index[2] == "c" && index[0] != candidate[0] {
				continue
			}
			actual, err := blogSchemaRows(ctx, tx, keys, candidate[0])
			if err != nil {
				return err
			}
			if reflect.DeepEqual(columns, actual) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("incompatible blog schema: missing or incompatible index %s on %s", index[0], table)
		}
	}
	return nil
}

type blogSchemaReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func compareBlogSchemaRows(ctx context.Context, actual, expected blogSchemaReader, query, name string) error {
	want, err := blogSchemaRows(ctx, expected, query, name)
	if err != nil {
		return err
	}
	got, err := blogSchemaRows(ctx, actual, query, name)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(want, got) {
		return fmt.Errorf("structure differs from the required schema")
	}
	return nil
}

// blogSchemaRows reads only SQLite schema metadata, never application content.
func blogSchemaRows(ctx context.Context, db blogSchemaReader, query string, args ...any) ([][]any, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var result [][]any
	for rows.Next() {
		values, targets := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		result = append(result, values)
	}
	return result, rows.Err()
}
