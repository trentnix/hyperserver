package blog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// insertImageReferences records a new version's references in its save transaction.
func insertImageReferences(ctx context.Context, tx *sql.Tx, table string, id int64, refs []string) error {
	for _, ref := range refs {
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+" (version_id, url) VALUES (?, ?)", id, ref); err != nil {
			return err
		}
	}
	return nil
}

// Resolve matching references through their version's owner, using primary-key
// lookups rather than scanning current content for a matching version pointer.
const publicImageQuery = publishedPages + `SELECT
EXISTS(SELECT 1 FROM blog_post_images i
 JOIN blog_posts r ON r.id=i.version_id
 JOIN blog_current_posts p ON p.id=r.owner_id AND p.version_id=r.id
 WHERE i.url IN (%[1]s) AND p.draft=0 AND p.published<=?)
OR EXISTS(SELECT 1 FROM blog_page_images i
 JOIN blog_pages r ON r.id=i.version_id
 JOIN blog_current_pages p ON p.id=r.owner_id AND p.version_id=r.id
 JOIN visible v ON v.id=p.id WHERE i.url IN (%[1]s))`

// ImageIsPublic checks both content types in one database snapshot. Page references
// count only when every ancestor is published. Shared images stay public while
// any public content still references them.
func (s *sqliteRepository) ImageIsPublic(ctx context.Context, references ...string) (bool, error) {
	if len(references) == 0 {
		return false, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(references)), ",")
	args := make([]any, 0, 2*len(references)+1)
	for i := range 2 {
		for _, reference := range references {
			args = append(args, reference)
		}
		if i == 0 {
			args = append(args, s.now().Unix())
		}
	}

	var public bool
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(publicImageQuery, placeholders), args...).Scan(&public)
	return public, err
}
