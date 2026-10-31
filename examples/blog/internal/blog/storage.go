package blog

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/trentnix/hyperserver/config"
	dbsqlite "github.com/trentnix/hyperserver/pkg/database/sqlite"
)

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
	if err == nil {
		err = initializeProfileRevisions(ctx, db)
	}
	if err == nil {
		err = initializeMediaTags(ctx, db)
	}
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return &sqliteRepository{db: db, now: time.Now}, db.Close, nil
}
