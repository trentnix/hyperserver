package blog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"time"

	"github.com/mattn/go-sqlite3"
)

const pageColumns = "id, slug, title, markdown, published, COALESCE(parent_id, ''), summary, preview_image, children_display, display_order, updated, ever_published, version, tags"

// publishedPages includes only pages reachable through published ancestors.
const publishedPages = `WITH RECURSIVE visible(id) AS (
 SELECT id FROM blog_page_current WHERE parent_id IS NULL AND published=1
 UNION ALL SELECT p.id FROM blog_page_current p JOIN visible v ON p.parent_id=v.id WHERE p.published=1
) `

// scanPage decodes pageColumns and maps absent records to errNotFound.
func scanPage(row interface{ Scan(...any) error }) (Page, error) {
	var p Page
	var updated int64
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Markdown, &p.Published, &p.ParentID, &p.Summary, &p.PreviewImage, &p.ChildrenDisplay, &p.DisplayOrder, &updated, &p.EverPublished, &p.Version, &p.Tags)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	p.Updated = time.Unix(0, updated).UTC()
	return p, err
}

// ListPages lists visible pages, explicit order first, then most recently updated.
// Markdown bodies are omitted. includeDrafts allows administrator access.
func (s *sqliteRepository) ListPages(ctx context.Context, includeDrafts bool) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx, publishedPages+`SELECT id, slug, title, '', published, COALESCE(parent_id, ''), summary, preview_image, children_display, display_order, updated, ever_published, version, ''
FROM blog_page_current WHERE ? OR id IN (SELECT id FROM visible)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id, slug, title, '', published, COALESCE(parent_id, ''), summary, preview_image, children_display, display_order, updated, ever_published, version, tags FROM blog_page_current ORDER BY display_order IS NULL, display_order, updated DESC, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	match := newContentMatcher(query)
	var matches [4][]Page
	for rows.Next() {
		page, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		rank := match.rank(page.Title, page.Tags, page.Summary)
		if rank < 0 {
			found, err := s.matchBody(ctx, tx, match, "page", page.ID, page.Version)
			if err != nil {
				return nil, err
			}
			if found {
				rank = 3
			}
		}
		if rank >= 0 {
			page.Markdown = ""
			matches[rank] = append(matches[rank], page)
		}
	}
	var result []Page
	for _, group := range matches {
		result = append(result, group...)
	}
	return result, rows.Err()
}

// GetPageBySlug hides drafts and descendants of drafts from public readers.
func (s *sqliteRepository) GetPageBySlug(ctx context.Context, slug string, includeDrafts bool) (Page, error) {
	return scanPage(s.db.QueryRowContext(ctx, publishedPages+"SELECT "+pageColumns+" FROM blog_page_current WHERE slug=? AND (? OR id IN (SELECT id FROM visible))", slug, includeDrafts))
}

// GetPageByID hides drafts and descendants of drafts from public readers.
func (s *sqliteRepository) GetPageByID(ctx context.Context, id string, includeDrafts bool) (Page, error) {
	return scanPage(s.db.QueryRowContext(ctx, publishedPages+"SELECT "+pageColumns+" FROM blog_page_current WHERE id=? AND (? OR id IN (SELECT id FROM visible))", id, includeDrafts))
}

// SavePage creates a page when ID is empty, or updates an existing page atomically.
// The caller must supply a valid slug and the current Version for updates.
func (s *sqliteRepository) SavePage(ctx context.Context, p Page, refs []string) (Page, error) {
	if p.ChildrenDisplay == "" {
		p.ChildrenDisplay = "hidden"
	}
	if p.ID != "" {
		current, err := s.GetPageByID(ctx, p.ID, true)
		if err != nil {
			return p, err
		}
		if p.Version != current.Version {
			return p, errEditConflict
		}
		// Ignore server-owned fields and compare the optional order by value.
		candidate := p
		candidate.Updated, candidate.EverPublished, candidate.DisplayOrder = current.Updated, current.EverPublished, current.DisplayOrder
		sameOrder := p.DisplayOrder == nil && current.DisplayOrder == nil || p.DisplayOrder != nil && current.DisplayOrder != nil && *p.DisplayOrder == *current.DisplayOrder
		if sameOrder && candidate == current {
			return current, nil
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()

	p.Updated = time.Now().UTC()
	if p.ID == "" {
		p.ID = rand.Text()
		p.Version = 1
		p.EverPublished = p.Published
		_, err = tx.ExecContext(ctx, `INSERT INTO blog_current_pages
(id, version_id, slug, published, parent_id, children_display, display_order, updated, ever_published)
VALUES (?,0,?,?,NULLIF(?, ''),?,?,?,?)`, p.ID, p.Slug, p.Published, p.ParentID, p.ChildrenDisplay, p.DisplayOrder, p.Updated.UnixNano(), p.EverPublished)
	} else {
		err = tx.QueryRowContext(ctx, `UPDATE blog_current_pages SET slug=?,published=?,parent_id=NULLIF(?, ''),children_display=?,display_order=?,updated=?,ever_published=(ever_published OR ?),version=version+1
WHERE id=? AND version=? RETURNING ever_published,version`, p.Slug, p.Published, p.ParentID, p.ChildrenDisplay, p.DisplayOrder, p.Updated.UnixNano(), p.Published, p.ID, p.Version).Scan(&p.EverPublished, &p.Version)
		if errors.Is(err, sql.ErrNoRows) {
			err = editError(ctx, tx, "page", p.ID, errNotFound)
		}
	}
	var constraint sqlite3.Error
	if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
		err = errPageSlugTaken
	}
	if errors.As(err, &constraint) && (constraint.ExtendedCode == sqlite3.ErrConstraintForeignKey || constraint.ExtendedCode == sqlite3.ErrConstraintTrigger) {
		err = errPageParent
	}
	if err == nil {
		var number int64
		number, err = saveContentVersion(ctx, tx, "page", p.ID, Revision{Title: p.Title, Summary: p.Summary, Markdown: p.Markdown, PreviewImage: p.PreviewImage, Tags: p.Tags}, refs)
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE blog_current_pages SET version_id=? WHERE id=?", number, p.ID)
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	return p, err
}

// DeletePage removes a page without deleting images shared by other content.
func (s *sqliteRepository) DeletePage(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM blog_current_pages WHERE id=?", id)
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
