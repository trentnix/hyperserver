package blog

import (
	"context"
	"database/sql"
	"slices"
	"strings"
)

// initializeTags adds optional search metadata without changing existing content.
func initializeTags(ctx context.Context, tx *sql.Tx, tables ...string) error {
	for _, table := range tables {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name='tags')", table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			// Table names are fixed internal names, never user input.
			if _, err := tx.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN tags TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListTags returns unique labels from current posts and pages in alphabetical order.
func (s *sqliteRepository) ListTags(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT tags FROM blog_posts WHERE tags<>'' UNION SELECT tags FROM blog_pages WHERE tags<>''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[string]bool)
	var tags []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		for _, tag := range strings.Split(value, ", ") {
			if !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}
	slices.Sort(tags)
	return tags, rows.Err()
}
