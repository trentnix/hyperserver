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
		return "blog_posts", "blog_post_revisions", nil
	case "page":
		return "blog_pages", "blog_page_revisions", nil
	default:
		return "", "", errNotFound
	}
}

// initializeRevisions captures existing content once and adds optimistic edit versions.
// Tables, columns, and baseline snapshots commit together or roll back together.
func initializeRevisions(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, kind := range []string{"post", "page"} {
		owners, revisions, _ := revisionTable(kind)
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name='version')", owners).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if _, err := tx.ExecContext(ctx, "ALTER TABLE "+owners+" ADD COLUMN version INTEGER NOT NULL DEFAULT 1"); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+revisions+` (
id INTEGER PRIMARY KEY AUTOINCREMENT, owner_id TEXT NOT NULL REFERENCES `+owners+`(id) ON DELETE CASCADE,
title TEXT NOT NULL, summary TEXT NOT NULL, markdown TEXT NOT NULL, preview_image TEXT NOT NULL, recorded INTEGER NOT NULL
); CREATE INDEX IF NOT EXISTS `+revisions+"_owner ON "+revisions+"(owner_id, id)"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+revisions+` (owner_id, title, summary, markdown, preview_image, recorded)
SELECT id, title, summary, markdown, preview_image, ? FROM `+owners+` o
WHERE NOT EXISTS (SELECT 1 FROM `+revisions+" r WHERE r.owner_id=o.id)", time.Now().UTC().UnixNano()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// recordRevision appends content only when it differs from the latest snapshot.
// It shares the content save transaction, including image-reference updates.
func recordRevision(ctx context.Context, tx *sql.Tx, kind, id string) error {
	owners, revisions, err := revisionTable(kind)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+revisions+` (owner_id, title, summary, markdown, preview_image, recorded)
SELECT id, title, summary, markdown, preview_image, ? FROM `+owners+` o WHERE id=? AND NOT EXISTS (
 SELECT 1 FROM `+revisions+` r WHERE r.id=(SELECT MAX(id) FROM `+revisions+` WHERE owner_id=o.id)
 AND r.title=o.title AND r.summary=o.summary AND r.markdown=o.markdown AND r.preview_image=o.preview_image
)`, time.Now().UTC().UnixNano(), id)
	return err
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

// GetRevision requires both owner and revision IDs to prevent cross-content loads.
func (s *sqliteRepository) GetRevision(ctx context.Context, kind, id string, number int64) (Revision, error) {
	_, table, err := revisionTable(kind)
	if err != nil {
		return Revision{}, err
	}
	var r Revision
	var recorded int64
	err = s.db.QueryRowContext(ctx, "SELECT id, title, summary, markdown, preview_image, recorded FROM "+table+" WHERE owner_id=? AND id=?", id, number).
		Scan(&r.ID, &r.Title, &r.Summary, &r.Markdown, &r.PreviewImage, &recorded)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	r.RecordedAt = time.Unix(0, recorded).UTC()
	return r, err
}
