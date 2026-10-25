package blog

import (
	"context"
	"database/sql"
	"errors"
)

// GetProfile returns the stored profile or errNotFound if none has been saved.
func (s *sqliteRepository) GetProfile(ctx context.Context) (Profile, error) {
	var p Profile
	err := s.db.QueryRowContext(ctx, "SELECT name, bio, image_url, favicon_url, background_url FROM blog_profile WHERE id=1").Scan(&p.Name, &p.Bio, &p.ImageURL, &p.FaviconURL, &p.BackgroundURL)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, errNotFound
	}
	return p, err
}

// SaveProfile atomically creates or replaces the singleton public profile.
func (s *sqliteRepository) SaveProfile(ctx context.Context, p Profile) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO blog_profile (id, name, bio, image_url, favicon_url, background_url)
VALUES (1, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, bio=excluded.bio,
image_url=excluded.image_url, favicon_url=excluded.favicon_url, background_url=excluded.background_url`, p.Name, p.Bio, p.ImageURL, p.FaviconURL, p.BackgroundURL)
	return err
}
