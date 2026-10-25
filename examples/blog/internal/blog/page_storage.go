package blog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"

	"github.com/mattn/go-sqlite3"
)

var errPageSlugTaken = errors.New("that slug is already used by another page")

// Page is standalone Markdown content, separate from posts, projects, and feeds.
// Drafts are visible only to the administrator.
type Page struct {
	ID, Slug, Title, Markdown string
	Published                 bool
}

// URL retains the short About address and puts other pages under /page/.
func (p Page) URL() string {
	if p.Slug == "about" {
		return "/about"
	}
	return "/page/" + p.Slug
}

// pageRepository keeps standalone content independent from posts and projects.
// Slugs must remain unique under concurrent writes. Updates never insert missing IDs.
type pageRepository interface {
	ListPages(context.Context) ([]Page, error)
	GetPageBySlug(context.Context, string, bool) (Page, error)
	GetPageByID(context.Context, string) (Page, error)
	SavePage(context.Context, Page) (Page, error)
	DeletePage(context.Context, string) error
}

const pageColumns = "id, slug, title, markdown, published"

// scanPage decodes pageColumns and maps absent records to errNotFound.
func scanPage(row interface{ Scan(...any) error }) (Page, error) {
	var p Page
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Markdown, &p.Published)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	return p, err
}

// ListPages lists all pages for administration, ordered by title then slug.
// Markdown bodies are omitted. Public readers must use GetPageBySlug instead.
func (s *sqliteRepository) ListPages(ctx context.Context) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, slug, title, '', published FROM blog_pages ORDER BY title, slug")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pages []Page
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

// GetPageBySlug hides drafts unless includeDrafts is true for an administrator.
func (s *sqliteRepository) GetPageBySlug(ctx context.Context, slug string, includeDrafts bool) (Page, error) {
	return scanPage(s.db.QueryRowContext(ctx, "SELECT "+pageColumns+" FROM blog_pages WHERE slug=? AND (? OR published=1)", slug, includeDrafts))
}

// GetPageByID loads a page for administrator-only editing.
func (s *sqliteRepository) GetPageByID(ctx context.Context, id string) (Page, error) {
	return scanPage(s.db.QueryRowContext(ctx, "SELECT "+pageColumns+" FROM blog_pages WHERE id=?", id))
}

// SavePage creates a page when ID is empty, or updates an existing page atomically.
func (s *sqliteRepository) SavePage(ctx context.Context, p Page) (Page, error) {
	var err error
	if p.ID == "" {
		p.ID = rand.Text()
		_, err = s.db.ExecContext(ctx, "INSERT INTO blog_pages ("+pageColumns+") VALUES (?, ?, ?, ?, ?)", p.ID, p.Slug, p.Title, p.Markdown, p.Published)
	} else {
		p, err = scanPage(s.db.QueryRowContext(ctx, "UPDATE blog_pages SET slug=?, title=?, markdown=?, published=? WHERE id=? RETURNING "+pageColumns, p.Slug, p.Title, p.Markdown, p.Published, p.ID))
	}
	var constraint sqlite3.Error
	if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
		err = errPageSlugTaken
	}
	return p, err
}

// DeletePage removes a page without deleting images shared by other content.
func (s *sqliteRepository) DeletePage(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM blog_pages WHERE id=?", id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		err = errNotFound
	}
	return err
}
