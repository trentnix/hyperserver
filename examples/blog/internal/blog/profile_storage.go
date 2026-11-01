package blog

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// GetProfile returns the stored profile or errNotFound if none has been saved.
func (s *sqliteRepository) GetProfile(ctx context.Context) (Profile, error) {
	var p Profile
	err := s.db.QueryRowContext(ctx, "SELECT name, bio, image_url, favicon_url, background_url, version FROM blog_profile WHERE id=1").Scan(&p.Name, &p.Bio, &p.ImageURL, &p.FaviconURL, &p.BackgroundURL, &p.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, errNotFound
	}
	return p, err
}

// SaveProfile saves only against the expected version and records changed content
// in the same transaction. Unchanged content leaves the edit token and history intact.
func (s *sqliteRepository) SaveProfile(ctx context.Context, p Profile) error {
	if p.Version > 0 {
		current, err := s.GetProfile(ctx)
		if err != nil {
			return err
		}
		if p.Version != current.Version {
			return errEditConflict
		}
		if p.sameContent(current) {
			return errProfileUnchanged
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var result sql.Result
	if p.Version == 0 {
		result, err = tx.ExecContext(ctx, `INSERT INTO blog_profile (id, name, bio, image_url, favicon_url, background_url, version)
VALUES (1, ?, ?, ?, ?, ?, 1) ON CONFLICT(id) DO NOTHING`, p.Name, p.Bio, p.ImageURL, p.FaviconURL, p.BackgroundURL)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE blog_profile SET name=?, bio=?, image_url=?, favicon_url=?, background_url=?, version=version+1
WHERE id=1 AND version=?`, p.Name, p.Bio, p.ImageURL, p.FaviconURL, p.BackgroundURL, p.Version)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errEditConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO blog_profile_revisions (owner_id, name, bio, image_url, favicon_url, background_url, recorded)
SELECT id, name, bio, image_url, favicon_url, background_url, ? FROM blog_profile p
WHERE NOT EXISTS (SELECT 1 FROM blog_profile_revisions r WHERE r.id=(SELECT MAX(id) FROM blog_profile_revisions)
AND r.name=p.name AND r.bio=p.bio AND r.image_url=p.image_url AND r.favicon_url=p.favicon_url AND r.background_url=p.background_url)`, time.Now().UTC().UnixNano())
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ListProfileRevisions returns newest-first labels without loading image URLs or bios.
func (s *sqliteRepository) ListProfileRevisions(ctx context.Context) ([]ProfileRevision, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, recorded FROM blog_profile_revisions ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []ProfileRevision
	for rows.Next() {
		var v ProfileRevision
		var recorded int64
		if err := rows.Scan(&v.ID, &v.Name, &recorded); err != nil {
			return nil, err
		}
		v.RecordedAt = time.Unix(0, recorded).UTC()
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// GetProfileRevision reads immutable content without treating its ID as an edit token.
func (s *sqliteRepository) GetProfileRevision(ctx context.Context, id int64) (ProfileRevision, error) {
	var v ProfileRevision
	var recorded int64
	err := s.db.QueryRowContext(ctx, "SELECT id, name, bio, image_url, favicon_url, background_url, recorded FROM blog_profile_revisions WHERE id=?", id).
		Scan(&v.ID, &v.Name, &v.Bio, &v.ImageURL, &v.FaviconURL, &v.BackgroundURL, &recorded)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	v.RecordedAt = time.Unix(0, recorded).UTC()
	return v, err
}

// DeleteProfileRevision atomically protects the latest saved profile, including concurrent saves.
func (s *sqliteRepository) DeleteProfileRevision(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM blog_profile_revisions
WHERE id=? AND id < (SELECT MAX(id) FROM blog_profile_revisions)`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count > 0 {
		return err
	}
	if _, err := s.GetProfileRevision(ctx, id); err != nil {
		return err
	}
	return errCurrentRevision
}
