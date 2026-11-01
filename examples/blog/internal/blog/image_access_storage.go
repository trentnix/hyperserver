package blog

import (
	"context"
	"database/sql"
	"strings"
)

// replaceImageReferences uses a fixed internal table name and the content's save
// transaction so a failed edit cannot publish or revoke an image accidentally.
func replaceImageReferences(ctx context.Context, tx *sql.Tx, table, id string, refs []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE owner_id=?", id); err != nil {
		return err
	}
	for _, ref := range refs {
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+" (owner_id, url) VALUES (?, ?)", id, ref); err != nil {
			return err
		}
	}
	return nil
}

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
	err := s.db.QueryRowContext(ctx, publishedPages+`SELECT
EXISTS(SELECT 1 FROM blog_post_images i JOIN blog_posts p ON p.id=i.owner_id WHERE i.url IN (`+placeholders+`) AND p.draft=0 AND p.published<=?)
OR EXISTS(SELECT 1 FROM blog_page_images i JOIN visible v ON v.id=i.owner_id WHERE i.url IN (`+placeholders+`))`, args...).Scan(&public)
	return public, err
}
