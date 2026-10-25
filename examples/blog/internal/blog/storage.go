package blog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/config"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

var (
	errNotFound  = errors.New("blog content not found")
	errSlugTaken = errors.New("that slug is already used by another post")
)

// Post stores the original Markdown. Publication time does not change on edits.
type Post struct {
	ID, Slug, Title, Summary, Markdown string
	Published                          time.Time
}

// ArchiveEntry contains only the link, title, and date shown in the post archive.
type ArchiveEntry struct {
	Slug, Title string
	Published   time.Time
}

// repository describes the storage guarantees used by HTTP handlers.
// Slugs must be unique, including under concurrent writes.
type repository interface {
	// List supplies complete posts for RSS, newest first.
	List(context.Context) ([]Post, error)
	// Latest returns errNotFound when no posts have been published.
	Latest(context.Context) (Post, error)
	// ListArchive returns link metadata newest first, without post bodies.
	ListArchive(context.Context) ([]ArchiveEntry, error)
	// ListRecent returns at most limit newest entries. The limit must be positive.
	ListRecent(context.Context, int) ([]ArchiveEntry, error)
	// GetByID returns errNotFound when the stable post ID does not exist.
	GetByID(context.Context, string) (Post, error)
	// GetBySlug returns errNotFound when the slug does not exist.
	GetBySlug(context.Context, string) (Post, error)
	// Save creates when ID is empty and updates an existing post otherwise.
	// Updates preserve publication time. Conflicts return errSlugTaken.
	Save(context.Context, Post) (Post, error)
	// Delete removes a post or returns errNotFound. Uploaded images remain available.
	Delete(context.Context, string) error
}

// sqliteRepository stores posts, projects, and pages in separate blog tables.
type sqliteRepository struct{ db *sql.DB }

// openRepository keeps storage selection and schema setup inside the blog.
// On success, the caller owns the returned close function.
func openRepository(ctx context.Context, settings Settings, directory string) (*sqliteRepository, func() error, error) {
	if settings.Storage.Provider != "sqlite" {
		return nil, nil, errors.New("blog.storage.provider must be sqlite")
	}
	db, err := dbsqlite.OpenConfigured(config.DatabaseConfig{Driver: "sqlite3", Connection: settings.Storage.Connection}, directory)
	if err != nil {
		return nil, nil, err
	}
	_, err = db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS blog_posts (
 id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL,
 summary TEXT NOT NULL, markdown TEXT NOT NULL, published INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS blog_projects (
 id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL,
 summary TEXT NOT NULL, markdown TEXT NOT NULL, preview_image TEXT NOT NULL,
 display_order INTEGER, published INTEGER NOT NULL CHECK (published IN (0, 1)), updated INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS blog_pages (
 id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL,
 markdown TEXT NOT NULL, published INTEGER NOT NULL CHECK (published IN (0, 1))
);`)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return &sqliteRepository{db: db}, db.Close, nil
}

const postColumns = "id, slug, title, summary, markdown, published"

// scanPost decodes a row in postColumns order and maps missing rows to errNotFound.
func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	var published int64
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Markdown, &published)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	p.Published = time.Unix(published, 0).UTC()
	return p, err
}

// List returns complete posts newest first, using insertion order to break ties.
func (s *sqliteRepository) List(ctx context.Context) ([]Post, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+postColumns+" FROM blog_posts ORDER BY published DESC, rowid DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// Latest fetches only the newest post, or returns errNotFound for an empty blog.
func (s *sqliteRepository) Latest(ctx context.Context) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_posts ORDER BY published DESC, rowid DESC LIMIT 1"))
}

// ListArchive returns metadata in the same order as List without loading Markdown.
func (s *sqliteRepository) ListArchive(ctx context.Context) ([]ArchiveEntry, error) {
	return s.listArchive(ctx, 0)
}

// ListRecent limits the database query, not just the rendered navigation.
func (s *sqliteRepository) ListRecent(ctx context.Context, limit int) ([]ArchiveEntry, error) {
	if limit < 1 {
		return nil, errors.New("recent post limit must be greater than zero")
	}
	return s.listArchive(ctx, limit)
}

// listArchive reads link metadata. A zero limit selects the complete archive.
func (s *sqliteRepository) listArchive(ctx context.Context, limit int) ([]ArchiveEntry, error) {
	query := "SELECT slug, title, published FROM blog_posts ORDER BY published DESC, rowid DESC"
	var args []any
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ArchiveEntry
	for rows.Next() {
		var entry ArchiveEntry
		var published int64
		if err := rows.Scan(&entry.Slug, &entry.Title, &published); err != nil {
			return nil, err
		}
		entry.Published = time.Unix(published, 0).UTC()
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// GetByID looks up a stable post ID, which does not change when its slug changes.
func (s *sqliteRepository) GetByID(ctx context.Context, id string) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_posts WHERE id=?", id))
}

// GetBySlug looks up an exact slug and returns errNotFound when it is absent.
func (s *sqliteRepository) GetBySlug(ctx context.Context, slug string) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_posts WHERE slug=?", slug))
}

// Save inserts a new post when ID is empty, generating its ID and any omitted slug.
// Updates preserve publication time and fail if the post no longer exists.
// The database enforces slug uniqueness and conflicts return errSlugTaken.
func (s *sqliteRepository) Save(ctx context.Context, p Post) (Post, error) {
	var err error
	if p.ID == "" {
		p.ID = rand.Text()
		if p.Slug == "" {
			p.Slug = strings.ToLower(p.ID)
		}
		p.Published = time.Now().UTC().Truncate(time.Second)
		_, err = s.db.ExecContext(ctx, "INSERT INTO blog_posts ("+postColumns+") VALUES (?, ?, ?, ?, ?, ?)", p.ID, p.Slug, p.Title, p.Summary, p.Markdown, p.Published.Unix())
	} else {
		// RETURNING avoids a second lookup racing with another edit or deletion.
		p, err = scanPost(s.db.QueryRowContext(ctx, "UPDATE blog_posts SET slug=?, title=?, summary=?, markdown=? WHERE id=? RETURNING "+postColumns, p.Slug, p.Title, p.Summary, p.Markdown, p.ID))
	}
	var constraint sqlite3.Error
	if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
		err = errSlugTaken
	}
	return p, err
}

// Delete removes one post or returns errNotFound. Images are retained because
// other posts can reference them.
func (s *sqliteRepository) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM blog_posts WHERE id=?", id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return errNotFound
	}
	return err
}
