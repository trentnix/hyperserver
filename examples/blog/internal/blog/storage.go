package blog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/config"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

var (
	errNotFound  = errors.New("blog content not found")
	errSlugTaken = errors.New("that slug is already used by another post")
)

// Post stores the original Markdown and publication state. Published is the first
// publication time and stays unchanged when a post is edited or republished.
// PreviewImage is an optional upload ID used for sharing, not the article body.
type Post struct {
	ID, Slug, Title, Summary, Markdown, PreviewImage string
	Published                                        time.Time
	Draft                                            bool
}

// PreviewURL returns the selected sharing image, or an empty string for automatic selection.
func (p Post) PreviewURL() string {
	if p.PreviewImage == "" {
		return ""
	}
	return "/images/" + p.PreviewImage
}

// ArchiveEntry contains only the link, title, and date shown in the post archive.
type ArchiveEntry struct {
	Slug, Title string
	Published   time.Time
}

// repository describes the storage guarantees used by HTTP handlers.
// Slugs must be unique, including under concurrent writes.
type repository interface {
	// List supplies complete published posts for RSS, newest first.
	List(context.Context) ([]Post, error)
	// ListMetadata supplies all posts for administration, without summaries or Markdown.
	// Drafts come first, then published posts newest first.
	ListMetadata(context.Context) ([]Post, error)
	// Latest returns errNotFound when no posts have been published.
	Latest(context.Context) (Post, error)
	// ListArchive returns link metadata newest first, without post bodies.
	ListArchive(context.Context) ([]ArchiveEntry, error)
	// ListRecent returns at most limit newest entries. The limit must be positive.
	ListRecent(context.Context, int) ([]ArchiveEntry, error)
	// GetByID returns errNotFound for missing IDs and drafts unless includeDrafts is true.
	GetByID(context.Context, string, bool) (Post, error)
	// GetBySlug returns errNotFound for missing slugs and drafts unless includeDrafts is true.
	GetBySlug(context.Context, string, bool) (Post, error)
	// Save writes content and Draft together, creating a post when ID is empty.
	// The caller must supply a valid slug.
	// First publication assigns Published. Edits preserve it. Conflicts return errSlugTaken.
	Save(context.Context, Post) (Post, error)
	// SetPublished changes visibility without changing content. First publication
	// assigns Published. Repeated calls preserve it. Missing IDs return errNotFound.
	SetPublished(context.Context, string, bool) (Post, error)
	// Delete removes a post or returns errNotFound. Uploaded images remain available.
	Delete(context.Context, string) error
}

// sqliteRepository stores blog content and the site profile.
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
 summary TEXT NOT NULL, markdown TEXT NOT NULL, published INTEGER NOT NULL,
 draft INTEGER NOT NULL DEFAULT 0 CHECK (draft IN (0, 1)),
 preview_image TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS blog_pages (
 id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL,
 markdown TEXT NOT NULL, published INTEGER NOT NULL CHECK (published IN (0, 1)),
 parent_id TEXT REFERENCES blog_pages(id) ON DELETE RESTRICT,
 summary TEXT NOT NULL, preview_image TEXT NOT NULL,
 children_display TEXT NOT NULL CHECK (children_display IN ('hidden', 'list', 'blocks')),
 display_order INTEGER CHECK (display_order BETWEEN 0 AND 1000000), updated INTEGER NOT NULL,
 ever_published INTEGER NOT NULL DEFAULT 0 CHECK (ever_published IN (0, 1))
);
CREATE INDEX IF NOT EXISTS blog_pages_parent ON blog_pages(parent_id);
CREATE TRIGGER IF NOT EXISTS blog_pages_no_cycle BEFORE UPDATE OF parent_id ON blog_pages
WHEN NEW.parent_id IS NOT NULL BEGIN
 SELECT RAISE(ABORT, 'page parent cycle') WHERE NEW.id IN (
  WITH RECURSIVE ancestors(id, parent_id) AS (
   SELECT id, parent_id FROM blog_pages WHERE id=NEW.parent_id
   UNION ALL SELECT p.id, p.parent_id FROM blog_pages p JOIN ancestors a ON p.id=a.parent_id
  ) SELECT id FROM ancestors
 );
END;
CREATE TABLE IF NOT EXISTS blog_profile (
 id INTEGER PRIMARY KEY CHECK (id=1), name TEXT NOT NULL, bio TEXT NOT NULL,
 image_url TEXT NOT NULL, favicon_url TEXT NOT NULL, background_url TEXT NOT NULL
);`)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	// Existing installations contain only published posts. Preserve their content
	// and dates when adding draft support.
	var hasDraft bool
	err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('blog_posts') WHERE name='draft')").Scan(&hasDraft)
	if err == nil && !hasDraft {
		_, err = db.ExecContext(ctx, "ALTER TABLE blog_posts ADD COLUMN draft INTEGER NOT NULL DEFAULT 0 CHECK (draft IN (0, 1))")
	}
	if err == nil {
		err = initializePagePublicationHistory(ctx, db)
	}
	if err == nil {
		var hasPreview bool
		err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('blog_posts') WHERE name='preview_image')").Scan(&hasPreview)
		if err == nil && !hasPreview {
			_, err = db.ExecContext(ctx, "ALTER TABLE blog_posts ADD COLUMN preview_image TEXT NOT NULL DEFAULT ''")
		}
	}
	if err == nil {
		err = initializeImageReferences(ctx, db)
	}
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return &sqliteRepository{db: db}, db.Close, nil
}

const postColumns = "id, slug, title, summary, markdown, published, draft, preview_image"

// scanPost decodes a row in postColumns order and maps missing rows to errNotFound.
func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	var published int64
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Markdown, &published, &p.Draft, &p.PreviewImage)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	if published != 0 {
		p.Published = time.Unix(published, 0).UTC()
	}
	return p, err
}

// List returns complete posts newest first, using insertion order to break ties.
func (s *sqliteRepository) List(ctx context.Context) ([]Post, error) {
	return s.listPosts(ctx, "SELECT "+postColumns+" FROM blog_posts WHERE draft=0 ORDER BY published DESC, rowid DESC")
}

// ListMetadata returns drafts newest-created first, then posts newest-published first.
func (s *sqliteRepository) ListMetadata(ctx context.Context) ([]Post, error) {
	return s.listPosts(ctx, "SELECT id, slug, title, '', '', published, draft, '' FROM blog_posts ORDER BY draft DESC, CASE WHEN draft=0 THEN published END DESC, rowid DESC")
}

// listPosts reads either complete public posts or administration metadata.
func (s *sqliteRepository) listPosts(ctx context.Context, query string) ([]Post, error) {
	rows, err := s.db.QueryContext(ctx, query)
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
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_posts WHERE draft=0 ORDER BY published DESC, rowid DESC LIMIT 1"))
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
	query := "SELECT slug, title, published FROM blog_posts WHERE draft=0 ORDER BY published DESC, rowid DESC"
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

// GetByID looks up a stable post ID, hiding drafts unless includeDrafts is true.
func (s *sqliteRepository) GetByID(ctx context.Context, id string, includeDrafts bool) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_posts WHERE id=? AND (? OR draft=0)", id, includeDrafts))
}

// GetBySlug looks up an exact slug, hiding drafts unless includeDrafts is true.
func (s *sqliteRepository) GetBySlug(ctx context.Context, slug string, includeDrafts bool) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_posts WHERE slug=? AND (? OR draft=0)", slug, includeDrafts))
}

// Save atomically writes content and publication state, generating IDs for new posts.
// First publication assigns the date. Later saves preserve it.
// Updates fail if the post no longer exists.
// The database enforces slug uniqueness and conflicts return errSlugTaken.
func (s *sqliteRepository) Save(ctx context.Context, p Post) (Post, error) {
	refs, err := imageReferences(p.Markdown, "/post/s/"+p.Slug, p.PreviewImage)
	if err != nil {
		return p, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()

	if p.ID == "" {
		p.ID = rand.Text()
		var published int64
		p.Published = time.Time{}
		if !p.Draft {
			p.Published = time.Now().UTC().Truncate(time.Second)
			published = p.Published.Unix()
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO blog_posts ("+postColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?)", p.ID, p.Slug, p.Title, p.Summary, p.Markdown, published, p.Draft, p.PreviewImage)
	} else {
		// RETURNING avoids a second lookup racing with another edit or deletion.
		p, err = scanPost(tx.QueryRowContext(ctx, `UPDATE blog_posts SET slug=?, title=?, summary=?, markdown=?, draft=?, preview_image=?,
 published=CASE WHEN ? AND published=0 THEN ? ELSE published END WHERE id=? RETURNING `+postColumns,
			p.Slug, p.Title, p.Summary, p.Markdown, p.Draft, p.PreviewImage, !p.Draft, time.Now().UTC().Unix(), p.ID))
	}
	var constraint sqlite3.Error
	if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
		err = errSlugTaken
	}
	if err == nil {
		err = replaceImageReferences(ctx, tx, "blog_post_images", p.ID, refs)
	}
	if err == nil {
		err = tx.Commit()
	}
	return p, err
}

// SetPublished atomically changes visibility and assigns the first publication date.
func (s *sqliteRepository) SetPublished(ctx context.Context, id string, published bool) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "UPDATE blog_posts SET draft=?, published=CASE WHEN ? AND published=0 THEN ? ELSE published END WHERE id=? RETURNING "+postColumns,
		!published, published, time.Now().UTC().Unix(), id))
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
