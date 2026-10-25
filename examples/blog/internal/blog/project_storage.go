package blog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

var errProjectSlugTaken = errors.New("that slug is already used by another project")

// Project holds a project page and its separate listing image. A nil DisplayOrder
// uses update time instead. Drafts are visible only to the administrator.
type Project struct {
	ID, Slug, Title, Summary, Markdown, PreviewImage string
	DisplayOrder                                     *int
	Published                                        bool
	Updated                                          time.Time
}

// PreviewURL returns the uploaded card image or the bundled default.
// Detail pages do not render this image.
func (p Project) PreviewURL() string {
	if p.PreviewImage == "" {
		return "/assets/img/project-default.svg"
	}
	return "/images/" + p.PreviewImage
}

// projectRepository keeps project persistence independent from post feeds and archives.
// Slugs must remain unique under concurrent writes. Updates never create missing IDs.
type projectRepository interface {
	ListProjects(context.Context, bool) ([]Project, error)
	GetProjectBySlug(context.Context, string, bool) (Project, error)
	GetProjectByID(context.Context, string) (Project, error)
	SaveProject(context.Context, Project) (Project, error)
	DeleteProject(context.Context, string) error
}

const projectColumns = "id, slug, title, summary, markdown, preview_image, display_order, published, updated"

// scanProject decodes projectColumns and translates absent rows into errNotFound.
func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	var updated int64
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Markdown, &p.PreviewImage, &p.DisplayOrder, &p.Published, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	p.Updated = time.Unix(0, updated).UTC()
	return p, err
}

// ListProjects puts explicitly ordered cards first, lowest order first. Update
// time (newest first) orders the rest and breaks ties. ID breaks equal timestamps.
// It omits Markdown bodies. includeDrafts must be true only for an administrator.
func (s *sqliteRepository) ListProjects(ctx context.Context, includeDrafts bool) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, title, summary, '', preview_image, display_order, published, updated
FROM blog_projects WHERE (? OR published=1) ORDER BY display_order IS NULL, display_order, updated DESC, id`, includeDrafts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// GetProjectBySlug hides drafts unless the caller explicitly requests them.
func (s *sqliteRepository) GetProjectBySlug(ctx context.Context, slug string, includeDrafts bool) (Project, error) {
	return scanProject(s.db.QueryRowContext(ctx, "SELECT "+projectColumns+" FROM blog_projects WHERE slug=? AND (? OR published=1)", slug, includeDrafts))
}

// GetProjectByID loads a project for administrator-only editing.
func (s *sqliteRepository) GetProjectByID(ctx context.Context, id string) (Project, error) {
	return scanProject(s.db.QueryRowContext(ctx, "SELECT "+projectColumns+" FROM blog_projects WHERE id=?", id))
}

// SaveProject inserts when ID is empty and otherwise updates an existing project.
// Each successful save records the current UTC time, stored as Unix nanoseconds.
func (s *sqliteRepository) SaveProject(ctx context.Context, p Project) (Project, error) {
	var err error
	p.Updated = time.Now().UTC()
	if p.ID == "" {
		p.ID = rand.Text()
		if p.Slug == "" {
			p.Slug = strings.ToLower(p.ID)
		}
		_, err = s.db.ExecContext(ctx, "INSERT INTO blog_projects ("+projectColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			p.ID, p.Slug, p.Title, p.Summary, p.Markdown, p.PreviewImage, p.DisplayOrder, p.Published, p.Updated.UnixNano())
	} else {
		p, err = scanProject(s.db.QueryRowContext(ctx, "UPDATE blog_projects SET slug=?, title=?, summary=?, markdown=?, preview_image=?, display_order=?, published=?, updated=? WHERE id=? RETURNING "+projectColumns,
			p.Slug, p.Title, p.Summary, p.Markdown, p.PreviewImage, p.DisplayOrder, p.Published, p.Updated.UnixNano(), p.ID))
	}
	var constraint sqlite3.Error
	if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
		err = errProjectSlugTaken
	}
	return p, err
}

// DeleteProject removes a project, retaining images that other content may reference.
func (s *sqliteRepository) DeleteProject(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM blog_projects WHERE id=?", id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		err = errNotFound
	}
	return err
}
