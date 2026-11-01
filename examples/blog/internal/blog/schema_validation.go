package blog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// validateBlogSchema checks the repository's required declarations, not an exact
// schema fingerprint. Extra indexes are allowed. It does not audit stored values
// or every SQL constraint. Adoption checks existing foreign-key references too.
func validateBlogSchema(ctx context.Context, tx *sql.Tx) error {
	if err := validateBlogColumns(ctx, tx); err != nil {
		return err
	}
	for _, key := range []struct {
		table, name, columns string
		unique               bool
	}{
		{"blog_posts", "", "id", true},
		{"blog_posts", "", "slug", true},
		{"blog_pages", "", "id", true},
		{"blog_pages", "", "slug", true},
		{"blog_setup", "", "email", true},
		{"blog_media", "", "id", true},
		{"blog_post_images", "", "owner_id,url", true},
		{"blog_page_images", "", "owner_id,url", true},
		{"blog_pages", "blog_pages_parent", "parent_id", false},
		{"blog_post_images", "blog_post_images_url", "url", false},
		{"blog_page_images", "blog_page_images_url", "url", false},
		{"blog_post_revisions", "blog_post_revisions_owner", "owner_id,id", false},
		{"blog_page_revisions", "blog_page_revisions_owner", "owner_id,id", false},
	} {
		if err := requireBlogIndex(ctx, tx, key.table, key.name, key.columns, key.unique); err != nil {
			return err
		}
	}
	for _, fk := range []struct{ table, column, parent, onDelete string }{
		{"blog_pages", "parent_id", "blog_pages", "RESTRICT"},
		{"blog_post_images", "owner_id", "blog_posts", "CASCADE"},
		{"blog_page_images", "owner_id", "blog_pages", "CASCADE"},
		{"blog_post_revisions", "owner_id", "blog_posts", "CASCADE"},
		{"blog_page_revisions", "owner_id", "blog_pages", "CASCADE"},
		{"blog_profile_revisions", "owner_id", "blog_profile", "CASCADE"},
	} {
		var valid bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS (
 SELECT 1 FROM pragma_foreign_key_list(?) GROUP BY id
 HAVING count(*)=1 AND min("from")=? AND min("table")=? AND min("to")='id'
 AND min(on_delete)=? AND min(on_update)='NO ACTION'
)`, fk.table, fk.column, fk.parent, fk.onDelete).Scan(&valid)
		if err != nil {
			return fmt.Errorf("inspect blog foreign key %s.%s: %w", fk.table, fk.column, err)
		}
		if !valid {
			return fmt.Errorf("incompatible blog schema: %s.%s requires a foreign key to %s(id) with ON DELETE %s", fk.table, fk.column, fk.parent, fk.onDelete)
		}
	}
	return requireBlogPageCycleTrigger(ctx, tx)
}

// validateBlogColumns checks real tables and writable columns. Only generated
// integer IDs require a particular primary-key declaration. Other keys are
// checked separately so SQLite's generated index names do not matter.
func validateBlogColumns(ctx context.Context, tx *sql.Tx) error {
	tables := make(map[string]bool)
	for _, group := range []struct {
		table, columns, kind string
		notNull, rowID       bool
	}{
		{"blog_posts", "id", "TEXT", false, false},
		{"blog_posts", "slug title summary markdown preview_image tags", "TEXT", true, false},
		{"blog_posts", "published draft version", "INTEGER", true, false},
		{"blog_pages", "id parent_id", "TEXT", false, false},
		{"blog_pages", "slug title markdown summary preview_image children_display tags", "TEXT", true, false},
		{"blog_pages", "published updated ever_published version", "INTEGER", true, false},
		{"blog_pages", "display_order", "INTEGER", false, false},
		{"blog_profile", "id", "INTEGER", false, true},
		{"blog_profile", "name bio image_url favicon_url background_url", "TEXT", true, false},
		{"blog_profile", "version", "INTEGER", true, false},
		{"blog_setup", "email", "TEXT", false, false},
		{"blog_setup", "complete", "INTEGER", true, false},
		{"blog_media", "id", "TEXT", false, false},
		{"blog_media", "filename label tags", "TEXT", true, false},
		{"blog_media", "version", "INTEGER", true, false},
		{"blog_post_images", "owner_id url", "TEXT", true, false},
		{"blog_page_images", "owner_id url", "TEXT", true, false},
		{"blog_post_revisions", "id", "INTEGER", false, true},
		{"blog_post_revisions", "owner_id title summary markdown preview_image tags", "TEXT", true, false},
		{"blog_post_revisions", "recorded", "INTEGER", true, false},
		{"blog_page_revisions", "id", "INTEGER", false, true},
		{"blog_page_revisions", "owner_id title summary markdown preview_image tags", "TEXT", true, false},
		{"blog_page_revisions", "recorded", "INTEGER", true, false},
		{"blog_profile_revisions", "id", "INTEGER", false, true},
		{"blog_profile_revisions", "name bio image_url favicon_url background_url", "TEXT", true, false},
		{"blog_profile_revisions", "owner_id recorded", "INTEGER", true, false},
	} {
		if !tables[group.table] {
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, group.table).Scan(&exists); err != nil {
				return fmt.Errorf("inspect blog table %s: %w", group.table, err)
			}
			if !exists {
				return fmt.Errorf("incompatible blog schema: missing table %s", group.table)
			}
			tables[group.table] = true
		}
		for _, column := range strings.Fields(group.columns) {
			var kind string
			var notNull bool
			var hidden int
			err := tx.QueryRowContext(ctx, `SELECT type, "notnull", hidden FROM pragma_table_xinfo(?) WHERE name=?`, group.table, column).Scan(&kind, &notNull, &hidden)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("incompatible blog schema: missing column %s.%s", group.table, column)
			}
			if err != nil {
				return fmt.Errorf("inspect blog column %s.%s: %w", group.table, column, err)
			}
			if !strings.EqualFold(kind, group.kind) || notNull != group.notNull || hidden != 0 {
				return fmt.Errorf("incompatible blog schema: %s.%s requires a writable %s column with notnull=%t", group.table, column, group.kind, group.notNull)
			}
			if group.rowID {
				var valid bool
				if err := tx.QueryRowContext(ctx, `SELECT count(*)=1 AND min(name)=?
 AND NOT EXISTS(SELECT 1 FROM pragma_index_list(?) WHERE origin='pk')
 FROM pragma_table_info(?) WHERE pk>0`, column, group.table, group.table).Scan(&valid); err != nil {
					return fmt.Errorf("inspect blog primary key %s.%s: %w", group.table, column, err)
				}
				if !valid {
					return fmt.Errorf("incompatible blog schema: %s.%s requires an INTEGER PRIMARY KEY", group.table, column)
				}
			}
		}
	}
	// Inserts omit these values and rely on their defaults.
	for _, field := range []struct{ table, column, value string }{
		{"blog_pages", "version", "1"},
		{"blog_media", "tags", "''"},
	} {
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(dflt_value, '')=? FROM pragma_table_info(?) WHERE name=?`, field.value, field.table, field.column).Scan(&valid); err != nil {
			return fmt.Errorf("inspect %s.%s default: %w", field.table, field.column, err)
		}
		if !valid {
			return fmt.Errorf("incompatible blog schema: %s.%s requires DEFAULT %s", field.table, field.column, field.value)
		}
	}
	// Reject obsolete blog tables rather than silently leaving their content behind.
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND lower(name) GLOB 'blog_*' AND lower(name)<>'blog_schema_version' ORDER BY name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return err
		}
		if !tables[table] {
			return fmt.Errorf("incompatible blog schema: unsupported table %s", table)
		}
	}
	return rows.Err()
}

// requireBlogIndex checks an ordered, non-partial binary index. A blank name
// accepts any matching unique key, including SQLite's primary-key indexes.
func requireBlogIndex(ctx context.Context, tx *sql.Tx, table, name, columns string, unique bool) error {
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (
 SELECT 1 FROM pragma_index_list(?) i
 WHERE (?='' OR i.name=?) AND i."unique"=? AND i.partial=0
 AND (SELECT group_concat(name, ',') FROM (
  SELECT name FROM pragma_index_xinfo(i.name) WHERE key=1 ORDER BY seqno
 ))=?
 AND NOT EXISTS (SELECT 1 FROM pragma_index_xinfo(i.name) WHERE key=1 AND (name IS NULL OR coll<>'BINARY' OR "desc"<>0))
)`, table, name, name, unique, columns).Scan(&valid)
	if err != nil {
		return fmt.Errorf("inspect blog index on %s(%s): %w", table, columns, err)
	}
	if !valid {
		if name != "" {
			return fmt.Errorf("incompatible blog schema: index %s must cover %s(%s) without a filter, using BINARY comparison", name, table, columns)
		}
		return fmt.Errorf("incompatible blog schema: %s requires a non-partial unique key on (%s) with BINARY comparison", table, columns)
	}
	return nil
}

// requireBlogPageCycleTrigger compares the one required trigger with its SQL
// migration declaration. Whitespace and legacy IF NOT EXISTS are insignificant.
func requireBlogPageCycleTrigger(ctx context.Context, tx *sql.Tx) error {
	const prefix = "CREATE TRIGGER blog_pages_no_cycle"
	_, body, found := strings.Cut(blogSchemaV1, prefix)
	body, _, ended := strings.Cut(body, "\nEND;")
	if !found || !ended {
		return fmt.Errorf("embedded blog migration is missing %s", prefix)
	}
	var declaration string
	err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='trigger' AND name='blog_pages_no_cycle' AND tbl_name='blog_pages'`).Scan(&declaration)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("incompatible blog schema: missing trigger blog_pages_no_cycle")
	}
	if err != nil {
		return fmt.Errorf("inspect blog_pages_no_cycle: %w", err)
	}
	declaration = strings.Replace(declaration, "CREATE TRIGGER IF NOT EXISTS ", "CREATE TRIGGER ", 1)
	if strings.Join(strings.Fields(declaration), " ") != strings.Join(strings.Fields(prefix+body+"\nEND"), " ") {
		return fmt.Errorf("incompatible blog schema: trigger blog_pages_no_cycle differs from the supported page-cycle guard")
	}
	return nil
}
