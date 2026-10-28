package blog

import (
	"context"
	"database/sql"
	"strings"
)

// initializeImageReferences backfills each reference table once. References are
// derived from saved content and maintained in the same transaction as edits.
func initializeImageReferences(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, kind := range []string{"post", "page"} {
		table, contentTable := "blog_"+kind+"_images", "blog_"+kind+"s"
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", table).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		// Names come only from the two fixed kinds above, never from user input.
		if _, err := tx.ExecContext(ctx, "CREATE TABLE "+table+" (owner_id TEXT NOT NULL REFERENCES "+contentTable+"(id) ON DELETE CASCADE, url TEXT NOT NULL, PRIMARY KEY(owner_id, url)); CREATE INDEX "+table+"_url ON "+table+"(url)"); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT id, slug, markdown, preview_image FROM "+contentTable)
		if err != nil {
			return err
		}
		byOwner := make(map[string][]string)
		for rows.Next() {
			var id, slug, source, preview string
			if err = rows.Scan(&id, &slug, &source, &preview); err != nil {
				break
			}
			contentPath := "/post/s/" + slug
			if kind == "page" {
				contentPath = (Page{Slug: slug}).URL()
			}
			byOwner[id], err = imageReferences(source, contentPath, preview)
			if err != nil {
				break
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		for id, refs := range byOwner {
			if err := replaceImageReferences(ctx, tx, table, id, refs); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

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
