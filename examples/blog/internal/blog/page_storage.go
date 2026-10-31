package blog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"time"

	"github.com/mattn/go-sqlite3"
)

var (
	errPageSlugTaken   = errors.New("that slug is already used by another page")
	errPageParent      = errors.New("choose an existing parent other than this page or one of its subpages")
	errPageHasChildren = errors.New("move or delete this page's subpages before deleting it")
)

// Page holds Markdown content, optional preview metadata, and its place in the
// page tree. ChildrenDisplay controls previews in the body, not navigation.
// Draft pages and their descendants are visible only to the administrator.
type Page struct {
	ID, Slug, Title, Markdown                        string
	ParentID, Summary, PreviewImage, ChildrenDisplay string
	DisplayOrder                                     *int
	Published                                        bool
	EverPublished                                    bool // Remains true after the page returns to draft.
	Updated                                          time.Time
	Version                                          int64 // SavePage requires the current version when updating.
}

// URL retains the short About and Projects addresses.
func (p Page) URL() string {
	if p.Slug == "about" || p.Slug == "projects" {
		return "/" + p.Slug
	}
	return "/page/" + p.Slug
}

// PreviewURL supplies an uploaded image or the neutral preview for child listings.
func (p Page) PreviewURL() string {
	if p.PreviewImage == "" {
		return "/assets/img/project-default.svg"
	}
	return "/images/" + p.PreviewImage
}

// pageRepository stores pages independently of post feeds and archives. Providers
// must enforce unique slugs, existing parents, and an acyclic tree atomically.
// Updates never insert missing IDs. Parents with children cannot be deleted.
// Lookups and ListPages exclude drafts and their descendants unless includeDrafts
// is true. SearchPages is reserved for administrator management.
type pageRepository interface {
	ListPages(context.Context, bool) ([]Page, error)
	// SearchPages searches current pages, including drafts and descendants of drafts.
	// Literal case-insensitive matches rank title before summary before Markdown.
	SearchPages(context.Context, string) ([]Page, error)
	GetPageBySlug(context.Context, string, bool) (Page, error)
	GetPageByID(context.Context, string, bool) (Page, error)
	// SavePage retains EverPublished once published, regardless of caller-supplied history.
	// Updates require the current Version or return errEditConflict. Content changes
	// append a revision in the same transaction as the save and image references.
	SavePage(context.Context, Page) (Page, error)
	DeletePage(context.Context, string) error
}

const pageColumns = "id, slug, title, markdown, published, COALESCE(parent_id, ''), summary, preview_image, children_display, display_order, updated, ever_published, version"

// initializePagePublicationHistory preserves known publication state on existing
// installations. Existing drafts do not record whether they were published earlier.
func initializePagePublicationHistory(ctx context.Context, db *sql.DB) error {
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('blog_pages') WHERE name='ever_published')").Scan(&exists); err != nil || exists {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `ALTER TABLE blog_pages ADD COLUMN ever_published INTEGER NOT NULL DEFAULT 0 CHECK (ever_published IN (0, 1));
UPDATE blog_pages SET ever_published=published;`); err != nil {
		return err
	}
	return tx.Commit()
}

// publishedPages includes only pages reachable through published ancestors.
const publishedPages = `WITH RECURSIVE visible(id) AS (
 SELECT id FROM blog_pages WHERE parent_id IS NULL AND published=1
 UNION ALL SELECT p.id FROM blog_pages p JOIN visible v ON p.parent_id=v.id WHERE p.published=1
) `

// scanPage decodes pageColumns and maps absent records to errNotFound.
func scanPage(row interface{ Scan(...any) error }) (Page, error) {
	var p Page
	var updated int64
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Markdown, &p.Published, &p.ParentID, &p.Summary, &p.PreviewImage, &p.ChildrenDisplay, &p.DisplayOrder, &updated, &p.EverPublished, &p.Version)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	p.Updated = time.Unix(0, updated).UTC()
	return p, err
}

// ListPages lists visible pages, explicit order first, then most recently updated.
// Markdown bodies are omitted. includeDrafts allows administrator access.
func (s *sqliteRepository) ListPages(ctx context.Context, includeDrafts bool) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx, publishedPages+`SELECT id, slug, title, '', published, COALESCE(parent_id, ''), summary, preview_image, children_display, display_order, updated, ever_published, version
FROM blog_pages WHERE ? OR id IN (SELECT id FROM visible)
ORDER BY display_order IS NULL, display_order, updated DESC, id`, includeDrafts)
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

// SearchPages returns matching metadata in relevance order, retaining the normal
// page order for ties. It scans bodies without keeping them in the result list.
func (s *sqliteRepository) SearchPages(ctx context.Context, query string) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+pageColumns+" FROM blog_pages ORDER BY display_order IS NULL, display_order, updated DESC, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	match := contentMatcher(query)
	var matches [3][]Page
	for rows.Next() {
		page, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		if rank := match(page.Title, page.Summary, page.Markdown); rank >= 0 {
			page.Markdown = ""
			matches[rank] = append(matches[rank], page)
		}
	}
	return append(append(matches[0], matches[1]...), matches[2]...), rows.Err()
}

// GetPageBySlug hides drafts and descendants of drafts from public readers.
func (s *sqliteRepository) GetPageBySlug(ctx context.Context, slug string, includeDrafts bool) (Page, error) {
	return scanPage(s.db.QueryRowContext(ctx, publishedPages+"SELECT "+pageColumns+" FROM blog_pages WHERE slug=? AND (? OR id IN (SELECT id FROM visible))", slug, includeDrafts))
}

// GetPageByID hides drafts and descendants of drafts from public readers.
func (s *sqliteRepository) GetPageByID(ctx context.Context, id string, includeDrafts bool) (Page, error) {
	return scanPage(s.db.QueryRowContext(ctx, publishedPages+"SELECT "+pageColumns+" FROM blog_pages WHERE id=? AND (? OR id IN (SELECT id FROM visible))", id, includeDrafts))
}

// SavePage creates a page when ID is empty, or updates an existing page atomically.
// The caller must supply a valid slug and the current Version for updates.
func (s *sqliteRepository) SavePage(ctx context.Context, p Page) (Page, error) {
	refs, err := imageReferences(p.Markdown, p.URL(), p.PreviewImage)
	if err != nil {
		return p, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()

	p.Updated = time.Now().UTC()
	if p.ChildrenDisplay == "" {
		p.ChildrenDisplay = "hidden"
	}
	if p.ID == "" {
		p.ID = rand.Text()
		p.Version = 1
		p.EverPublished = p.Published
		_, err = tx.ExecContext(ctx, `INSERT INTO blog_pages
(id, slug, title, markdown, published, parent_id, summary, preview_image, children_display, display_order, updated, ever_published)
VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?)`, p.ID, p.Slug, p.Title, p.Markdown, p.Published, p.ParentID, p.Summary, p.PreviewImage, p.ChildrenDisplay, p.DisplayOrder, p.Updated.UnixNano(), p.EverPublished)
	} else {
		id := p.ID
		p, err = scanPage(tx.QueryRowContext(ctx, "UPDATE blog_pages SET slug=?, title=?, markdown=?, published=?, parent_id=NULLIF(?, ''), summary=?, preview_image=?, children_display=?, display_order=?, updated=?, ever_published=(ever_published OR ?), version=version+1 WHERE id=? AND version=? RETURNING "+pageColumns,
			p.Slug, p.Title, p.Markdown, p.Published, p.ParentID, p.Summary, p.PreviewImage, p.ChildrenDisplay, p.DisplayOrder, p.Updated.UnixNano(), p.Published, p.ID, p.Version))
		err = editError(ctx, tx, "page", id, err)
	}
	var constraint sqlite3.Error
	if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
		err = errPageSlugTaken
	}
	if errors.As(err, &constraint) && (constraint.ExtendedCode == sqlite3.ErrConstraintForeignKey || constraint.ExtendedCode == sqlite3.ErrConstraintTrigger) {
		err = errPageParent
	}
	if err == nil {
		err = replaceImageReferences(ctx, tx, "blog_page_images", p.ID, refs)
	}
	if err == nil {
		err = recordRevision(ctx, tx, "page", p.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	return p, err
}

// DeletePage removes a page without deleting images shared by other content.
func (s *sqliteRepository) DeletePage(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM blog_pages WHERE id=?", id)
	var constraint sqlite3.Error
	if errors.As(err, &constraint) && constraint.Code == sqlite3.ErrConstraint {
		return errPageHasChildren
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		err = errNotFound
	}
	return err
}
