// Package sqlite supplies blog storage using an application-owned database pool.
// A Repository implements both blog.Repository and blog.ImageStore. Call Initialize
// before serving requests. The application owns the pool's configuration and cleanup.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/modules/blog"
)

// Repository implements blog.Repository and blog.ImageStore using database/sql.
// It supports concurrent calls and never closes the application-owned pool.
type Repository struct{ db *sql.DB }

// New creates a repository using db without opening connections or creating tables.
// Call Initialize before use and keep db open until all repository calls finish.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

// Initialize creates blog_posts, blog_images, and the publication-order index in
// one transaction. Existing tables and data are preserved. It checks that required
// columns can be queried but does not migrate an incompatible existing schema.
// A nil database pool or a canceled context causes initialization to fail.
func (s *Repository) Initialize(ctx context.Context) error {
	if s.db == nil {
		return errors.New("blog requires a database pool")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS blog_posts (
		id TEXT PRIMARY KEY NOT NULL, slug TEXT NOT NULL UNIQUE,
		title TEXT NOT NULL, summary TEXT NOT NULL, markdown TEXT NOT NULL,
		published_at INTEGER NOT NULL, version INTEGER NOT NULL CHECK(version > 0));
		CREATE INDEX IF NOT EXISTS blog_posts_order ON blog_posts(published_at DESC, id ASC);
		CREATE TABLE IF NOT EXISTS blog_images (
		id TEXT PRIMARY KEY NOT NULL, media_type TEXT NOT NULL, data BLOB NOT NULL);`)
	if err != nil {
		return err
	}
	for _, query := range []string{
		"SELECT " + columns + " FROM blog_posts LIMIT 0",
		"SELECT id, media_type, data FROM blog_images LIMIT 0",
	} {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const columns = "id, slug, title, summary, markdown, published_at, version"

// scan shares decoding between single-row lookups and list results. Stored times
// use Unix nanoseconds and are returned in UTC. Invalid stored posts are rejected.
func scan(row interface{ Scan(...any) error }) (blog.Post, error) {
	var p blog.Post
	var published int64
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Markdown, &published, &p.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return p, blog.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.PublishedAt = time.Unix(0, published).UTC()
	return p, blog.Validate(p)
}

// GetByID reads a post by its stable ID.
// It returns blog.ErrNotFound when absent or blog.ErrInvalidPost for invalid stored fields.
func (s *Repository) GetByID(ctx context.Context, id string) (blog.Post, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM blog_posts WHERE id = ?", id))
}

// GetBySlug reads a post by its current URL slug.
// It returns blog.ErrNotFound when absent or blog.ErrInvalidPost for invalid stored fields.
func (s *Repository) GetBySlug(ctx context.Context, slug string) (blog.Post, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM blog_posts WHERE slug = ?", slug))
}

// List reads posts ordered by publication time descending, then ID ascending.
// Offset must be nonnegative, and limit must be between 1 and blog.MaxListSize.
// An empty page returns a nil slice without an error. Invalid stored posts fail the read.
func (s *Repository) List(ctx context.Context, offset, limit int) ([]blog.Post, error) {
	if offset < 0 || limit < 1 || limit > blog.MaxListSize {
		return nil, errors.New("invalid blog list bounds")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+" FROM blog_posts ORDER BY published_at DESC, id ASC LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []blog.Post
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// Create inserts a post at version 1 with a unique ID and slug.
// Invalid posts return blog.ErrInvalidPost. An existing ID returns blog.ErrConflict,
// and an existing slug returns blog.ErrSlugExists.
// If both conflict, either error can be returned.
func (s *Repository) Create(ctx context.Context, p blog.Post) error {
	if err := blog.Validate(p); err != nil {
		return err
	}
	if p.Version != 1 {
		return blog.ErrInvalidPost
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO blog_posts ("+columns+") VALUES (?, ?, ?, ?, ?, ?, ?)",
		p.ID, p.Slug, p.Title, p.Summary, p.Markdown, p.PublishedAt.UnixNano(), p.Version)
	return postError(err)
}

// Update replaces editable fields only when the stored version matches p.Version.
// Success increments the stored version and preserves the ID and publication time.
// Missing posts, stale versions, and exhausted version counters return blog.ErrConflict.
// Invalid posts return blog.ErrInvalidPost. Duplicate slugs return blog.ErrSlugExists.
func (s *Repository) Update(ctx context.Context, p blog.Post) error {
	if err := blog.Validate(p); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE blog_posts SET slug=?, title=?, summary=?, markdown=?, version=version+1
		WHERE id=? AND version=? AND version < 9223372036854775807`,
		p.Slug, p.Title, p.Summary, p.Markdown, p.ID, p.Version)
	if err != nil {
		return postError(err)
	}
	return changed(result)
}

// Delete removes the matching post version without removing uploaded images.
// A missing post or version mismatch returns blog.ErrConflict.
func (s *Repository) Delete(ctx context.Context, id string, version int64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM blog_posts WHERE id=? AND version=?", id, version)
	if err != nil {
		return err
	}
	return changed(result)
}

// changed distinguishes a successful version-checked write from a stale or missing post.
func changed(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return blog.ErrConflict
	}
	return nil
}

// postError translates constraint failures into the storage contract's errors.
func postError(err error) error {
	var constraint sqlite3.Error
	if errors.As(err, &constraint) {
		if constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
			return blog.ErrSlugExists
		}
		if constraint.ExtendedCode == sqlite3.ErrConstraintPrimaryKey {
			return blog.ErrConflict
		}
	}
	return err
}

// Save inserts an immutable image after the caller validates its encoded content.
// It checks the UUID, byte length, and supported media type but does not decode Data.
// An existing ID returns blog.ErrConflict and leaves the original image unchanged.
func (s *Repository) Save(ctx context.Context, image blog.Image) error {
	if _, err := uuid.Parse(image.ID); err != nil {
		return err
	}
	if len(image.Data) == 0 || len(image.Data) > blog.MaxImageBytes ||
		(image.MediaType != "image/png" && image.MediaType != "image/jpeg" && image.MediaType != "image/gif") {
		return errors.New("invalid blog image")
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO blog_images(id, media_type, data) VALUES (?, ?, ?)", image.ID, image.MediaType, image.Data)
	return postError(err)
}

// Get reads an image into request-owned bytes. Oversized stored data is rejected
// in SQL before the driver copies it into Go memory.
// It returns blog.ErrNotFound for a missing ID and an error for empty stored data.
func (s *Repository) Get(ctx context.Context, id string) (blog.Image, error) {
	var image blog.Image
	err := s.db.QueryRowContext(ctx, `SELECT id, media_type,
		CASE WHEN length(data) BETWEEN 1 AND ? THEN data ELSE NULL END
		FROM blog_images WHERE id=?`, blog.MaxImageBytes, id).Scan(&image.ID, &image.MediaType, &image.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return image, blog.ErrNotFound
	}
	if err != nil {
		return image, fmt.Errorf("read blog image: %w", err)
	}
	if len(image.Data) == 0 {
		return blog.Image{}, errors.New("invalid stored blog image size")
	}
	return image, nil
}
