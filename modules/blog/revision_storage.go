package blog

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// revisionTable selects fixed table names. Request values never become SQL identifiers.
func revisionTable(kind string) (string, string, error) {
	switch kind {
	case "post":
		return "blog_current_posts", "blog_posts", nil
	case "page":
		return "blog_current_pages", "blog_pages", nil
	default:
		return "", "", errNotFound
	}
}

// saveContentVersion reuses unchanged writing or inserts a new immutable version
// and its media references. The caller locks the current row before calling it.
func saveContentVersion(ctx context.Context, tx *sql.Tx, kind, id string, content Revision, refs []string) (int64, error) {
	owners, revisions, err := revisionTable(kind)
	if err != nil {
		return 0, err
	}
	var slug string
	if err := tx.QueryRowContext(ctx, "SELECT slug FROM "+owners+" WHERE id=?", id).Scan(&slug); err != nil {
		return 0, err
	}
	path := "/post/s/" + slug
	media := "blog_post_media"
	if kind == "page" {
		path = (Page{Slug: slug}).URL()
		media = "blog_page_media"
	}
	// Relative links and images depend on the URL base as well as the writing.
	// Both must match before reusing a version and its derived references.
	var number int64
	err = tx.QueryRowContext(ctx, "SELECT r.id FROM "+revisions+" r JOIN "+owners+` c ON c.version_id=r.id
WHERE c.id=? AND r.title=? AND r.summary=? AND r.markdown=? AND r.preview_image=? AND r.tags=? AND r.content_path=?`,
		id, content.Title, content.Summary, content.Markdown, content.PreviewImage, content.Tags, path).Scan(&number)
	if err == nil {
		return number, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO "+revisions+` (owner_id,title,summary,markdown,preview_image,tags,recorded,content_path) VALUES(?,?,?,?,?,?,?,?)`,
		id, content.Title, content.Summary, content.Markdown, content.PreviewImage, content.Tags, time.Now().UTC().UnixNano(), path)
	if err != nil {
		return 0, err
	}
	number, err = result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return number, insertMediaReferences(ctx, tx, media, number, refs)
}

// editError distinguishes deleted content from a stale editor after a conditional update.
func editError(ctx context.Context, tx *sql.Tx, kind, id string, err error) error {
	if !errors.Is(err, errNotFound) {
		return err
	}
	owners, _, tableError := revisionTable(kind)
	if tableError != nil {
		return tableError
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+owners+" WHERE id=?)", id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errEditConflict
	}
	return errNotFound
}

// HasHistory reports whether content has an earlier saved version. The index
// lookup stops after two snapshots, regardless of the total history size.
func (s *sqliteRepository) HasHistory(ctx context.Context, kind, id string) (bool, error) {
	_, table, err := revisionTable(kind)
	if err != nil {
		return false, err
	}
	var available bool
	err = s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE owner_id=? LIMIT 1 OFFSET 1)", id).Scan(&available)
	return available, err
}

// HistoryOwners identifies content with earlier versions for management lists,
// without querying once per row or loading snapshot contents.
func (s *sqliteRepository) HistoryOwners(ctx context.Context, kind string) (map[string]bool, error) {
	_, table, err := revisionTable(kind)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT owner_id FROM "+table+" GROUP BY owner_id HAVING count(*) > 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		owners[id] = true
	}
	return owners, rows.Err()
}

// ListRevisions returns newest-first metadata without loading Markdown bodies.
func (s *sqliteRepository) ListRevisions(ctx context.Context, kind, id string) ([]Revision, error) {
	_, table, err := revisionTable(kind)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, title, recorded FROM "+table+" WHERE owner_id=? ORDER BY id DESC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Revision
	for rows.Next() {
		var r Revision
		var recorded int64
		if err := rows.Scan(&r.ID, &r.Title, &recorded); err != nil {
			return nil, err
		}
		r.RecordedAt = time.Unix(0, recorded).UTC()
		result = append(result, r)
	}
	return result, rows.Err()
}

// DeleteRevision removes an older snapshot without changing its owner or edit token.
// The latest-version guard is part of the delete so concurrent saves and deletes
// cannot remove the current snapshot.
func (s *sqliteRepository) DeleteRevision(ctx context.Context, kind, id string, number int64) error {
	owners, table, err := revisionTable(kind)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM "+table+" WHERE owner_id=? AND id=? AND id <> (SELECT version_id FROM "+owners+" WHERE id=?)", id, number, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count > 0 {
		return err
	}
	if _, err := s.GetRevision(ctx, kind, id, number); err != nil {
		return err
	}
	return errCurrentRevision
}

// GetRevision requires both owner and revision IDs to prevent cross-content loads.
func (s *sqliteRepository) GetRevision(ctx context.Context, kind, id string, number int64) (Revision, error) {
	_, table, err := revisionTable(kind)
	if err != nil {
		return Revision{}, err
	}
	var r Revision
	var recorded int64
	err = s.db.QueryRowContext(ctx, "SELECT id, title, summary, markdown, preview_image, tags, recorded, content_path FROM "+table+" WHERE owner_id=? AND id=?", id, number).
		Scan(&r.ID, &r.Title, &r.Summary, &r.Markdown, &r.PreviewImage, &r.Tags, &recorded, &r.ContentPath)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	r.RecordedAt = time.Unix(0, recorded).UTC()
	return r, err
}
