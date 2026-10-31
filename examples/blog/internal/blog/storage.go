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
	errNotFound         = errors.New("blog content not found")
	errSlugTaken        = errors.New("that slug is already used by another post")
	errAlreadyPublished = errors.New("only posts that have never been published can be scheduled")
)

// Post stores the original Markdown and publication state. Published is the first
// publication time, or the planned time for a scheduled post. Once reached, that
// date stays unchanged when a post is edited or republished.
// PreviewImage is an optional upload ID used for sharing, not the article body.
type Post struct {
	ID, Slug, Title, Summary, Markdown, PreviewImage string
	Published                                        time.Time
	Draft                                            bool
	Version                                          int64 // Save requires the current version when updating.
}

// Scheduled reports whether the post is awaiting its first publication.
func (p Post) Scheduled(now time.Time) bool { return !p.Draft && p.Published.After(now) }

// WasPublished distinguishes past publication from a future schedule.
func (p Post) WasPublished(now time.Time) bool {
	return !p.Published.IsZero() && !p.Published.After(now)
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
	// Drafts come first, then scheduled and published posts newest first.
	ListMetadata(context.Context) ([]Post, error)
	// SearchPosts searches all publication states, returning metadata only.
	// Literal case-insensitive matches rank title before summary before Markdown.
	SearchPosts(context.Context, string) ([]Post, error)
	// Latest returns errNotFound when no posts have been published.
	Latest(context.Context) (Post, error)
	// ListArchive returns link metadata newest first, without post bodies.
	ListArchive(context.Context) ([]ArchiveEntry, error)
	// ListArchivePage returns at most limit public entries after offset entries.
	// The limit must be positive and the offset nonnegative. Ordering matches ListArchive.
	ListArchivePage(context.Context, int, int) ([]ArchiveEntry, error)
	// ListRecent returns at most limit newest entries. The limit must be positive.
	ListRecent(context.Context, int) ([]ArchiveEntry, error)
	// NextPublication returns the next scheduled time, or zero if none remain.
	NextPublication(context.Context) (time.Time, error)
	// GetByID hides drafts and scheduled posts unless includeDrafts is true.
	GetByID(context.Context, string, bool) (Post, error)
	// GetBySlug hides drafts and scheduled posts unless includeDrafts is true.
	GetBySlug(context.Context, string, bool) (Post, error)
	// Save writes content and Draft together, creating a post when ID is empty.
	// The caller must supply a valid slug.
	// A future Published schedules first publication. Once reached, that date stays
	// unchanged. Rescheduling published content returns errAlreadyPublished.
	// Conflicting slugs return errSlugTaken.
	// Updates require the current Version or return errEditConflict. Content changes
	// append a revision in the same transaction as the save and image references.
	Save(context.Context, Post) (Post, error)
	// SetPublished immediately publishes or hides content, replacing any pending
	// schedule. Past publication dates stay unchanged. Missing IDs return errNotFound.
	SetPublished(context.Context, string, bool) (Post, error)
	// Delete removes a post or returns errNotFound. Uploaded images remain available.
	Delete(context.Context, string) error
}

// sqliteRepository stores blog content and the site profile.
type sqliteRepository struct {
	db  *sql.DB
	now func() time.Time
}

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
);
CREATE TABLE IF NOT EXISTS blog_media (
 id TEXT PRIMARY KEY, filename TEXT NOT NULL, label TEXT NOT NULL,
 version INTEGER NOT NULL CHECK (version>0)
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
	if err == nil {
		err = initializeRevisions(ctx, db)
	}
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return &sqliteRepository{db: db, now: time.Now}, db.Close, nil
}

const postColumns = "id, slug, title, summary, markdown, published, draft, preview_image, version"

// scanPost decodes a row in postColumns order and maps missing rows to errNotFound.
func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	var published int64
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Markdown, &published, &p.Draft, &p.PreviewImage, &p.Version)
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
	return s.listPosts(ctx, "SELECT "+postColumns+" FROM blog_posts WHERE draft=0 AND published<=? ORDER BY published DESC, rowid DESC", s.now().Unix())
}

// ListMetadata returns drafts newest-created first, then posts newest-published first.
func (s *sqliteRepository) ListMetadata(ctx context.Context) ([]Post, error) {
	return s.listPosts(ctx, "SELECT id, slug, title, '', '', published, draft, '', version FROM blog_posts ORDER BY draft DESC, CASE WHEN draft=0 THEN published END DESC, rowid DESC")
}

// SearchPosts scans current content with Unicode case folding. Only matching
// metadata is retained. Equal ranks preserve the management list's order.
func (s *sqliteRepository) SearchPosts(ctx context.Context, query string) ([]Post, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+postColumns+" FROM blog_posts ORDER BY draft DESC, CASE WHEN draft=0 THEN published END DESC, rowid DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	match := contentMatcher(query)
	var matches [3][]Post
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		if rank := match(post.Title, post.Summary, post.Markdown); rank >= 0 {
			post.Summary, post.Markdown, post.PreviewImage = "", "", ""
			matches[rank] = append(matches[rank], post)
		}
	}
	return append(append(matches[0], matches[1]...), matches[2]...), rows.Err()
}

// listPosts reads either complete public posts or administration metadata.
func (s *sqliteRepository) listPosts(ctx context.Context, query string, args ...any) ([]Post, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
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
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_posts WHERE draft=0 AND published<=? ORDER BY published DESC, rowid DESC LIMIT 1", s.now().Unix()))
}

// NextPublication bounds the lifetime of public caches without a background timer.
func (s *sqliteRepository) NextPublication(ctx context.Context) (time.Time, error) {
	var next sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT MIN(published) FROM blog_posts WHERE draft=0 AND published>?", s.now().Unix()).Scan(&next)
	if err != nil || !next.Valid {
		return time.Time{}, err
	}
	return time.Unix(next.Int64, 0).UTC(), nil
}

// ListArchive returns metadata in the same order as List without loading Markdown.
func (s *sqliteRepository) ListArchive(ctx context.Context) ([]ArchiveEntry, error) {
	return s.listArchive(ctx, 0, 0)
}

// ListArchivePage bounds the metadata read in SQLite, including publication visibility.
func (s *sqliteRepository) ListArchivePage(ctx context.Context, limit, offset int) ([]ArchiveEntry, error) {
	if limit < 1 || offset < 0 {
		return nil, errors.New("archive limit must be positive and offset must be nonnegative")
	}
	return s.listArchive(ctx, limit, offset)
}

// ListRecent limits the database query, not just the rendered navigation.
func (s *sqliteRepository) ListRecent(ctx context.Context, limit int) ([]ArchiveEntry, error) {
	if limit < 1 {
		return nil, errors.New("recent post limit must be greater than zero")
	}
	return s.listArchive(ctx, limit, 0)
}

// listArchive reads link metadata. A zero limit selects the complete archive.
func (s *sqliteRepository) listArchive(ctx context.Context, limit, offset int) ([]ArchiveEntry, error) {
	query := "SELECT slug, title, published FROM blog_posts WHERE draft=0 AND published<=? ORDER BY published DESC, rowid DESC"
	args := []any{s.now().Unix()}
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
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

// GetByID hides drafts and scheduled posts unless includeDrafts is true.
func (s *sqliteRepository) GetByID(ctx context.Context, id string, includeDrafts bool) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_posts WHERE id=? AND (? OR (draft=0 AND published<=?))", id, includeDrafts, s.now().Unix()))
}

// GetBySlug hides drafts and scheduled posts unless includeDrafts is true.
func (s *sqliteRepository) GetBySlug(ctx context.Context, slug string, includeDrafts bool) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM blog_posts WHERE slug=? AND (? OR (draft=0 AND published<=?))", slug, includeDrafts, s.now().Unix()))
}

// Save atomically writes content and publication state, generating IDs for new posts.
// A future Published schedules first publication. Once reached, that date stays unchanged.
// Updates require the current Version and fail if the post no longer exists.
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

	now := s.now().UTC().Truncate(time.Second)
	var publication int64
	if !p.Draft {
		publication = now.Unix()
		if p.Published.After(now) {
			publication = p.Published.Unix()
		}
	}
	if p.ID == "" {
		p.ID = rand.Text()
		var published int64
		p.Published = time.Time{}
		if !p.Draft {
			p.Published = time.Unix(publication, 0).UTC()
			published = p.Published.Unix()
		}
		p.Version = 1
		_, err = tx.ExecContext(ctx, "INSERT INTO blog_posts ("+postColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", p.ID, p.Slug, p.Title, p.Summary, p.Markdown, published, p.Draft, p.PreviewImage, p.Version)
	} else {
		// RETURNING avoids a second lookup racing with another edit or deletion.
		id := p.ID
		p, err = scanPost(tx.QueryRowContext(ctx, `UPDATE blog_posts SET slug=?, title=?, summary=?, markdown=?, draft=?, preview_image=?, version=version+1,
 published=CASE WHEN published>0 AND published<=? THEN published ELSE ? END WHERE id=? AND version=? RETURNING `+postColumns,
			p.Slug, p.Title, p.Summary, p.Markdown, p.Draft, p.PreviewImage, now.Unix(), publication, p.ID, p.Version))
		err = editError(ctx, tx, "post", id, err)
		if err == nil && publication > now.Unix() && p.WasPublished(now) {
			return p, errAlreadyPublished
		}
	}
	var constraint sqlite3.Error
	if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
		err = errSlugTaken
	}
	if err == nil {
		err = replaceImageReferences(ctx, tx, "blog_post_images", p.ID, refs)
	}
	if err == nil {
		err = recordRevision(ctx, tx, "post", p.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	return p, err
}

// SetPublished immediately publishes or hides content, replacing any pending schedule.
func (s *sqliteRepository) SetPublished(ctx context.Context, id string, published bool) (Post, error) {
	now := s.now().Unix()
	var date int64
	if published {
		date = now
	}
	return scanPost(s.db.QueryRowContext(ctx, "UPDATE blog_posts SET draft=?, version=version+1, published=CASE WHEN published>0 AND published<=? THEN published ELSE ? END WHERE id=? RETURNING "+postColumns,
		!published, now, date, id))
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
