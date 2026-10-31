package blog

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"
)

var (
	errPageSlugTaken   = errors.New("that slug is already used by another page")
	errPageParent      = errors.New("choose an existing parent other than this page or one of its subpages")
	errPageHasChildren = errors.New("move or delete this page's subpages before deleting it")
)

const defaultPagePreviewURL = "/assets/img/project-default.svg"

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
		return defaultPagePreviewURL
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
	// The caller supplies rendered image references for the same content.
	// Unchanged content and settings preserve Version, Updated, and history.
	SavePage(context.Context, Page, []string) (Page, error)
	DeletePage(context.Context, string) error
}

// validatePage returns the first invalid-field message, or an empty string.
func validatePage(p Page) string {
	if p.Title == "" || !utf8.ValidString(p.Title) || utf8.RuneCountInString(p.Title) > 240 {
		return "Enter a title of at most 240 characters."
	}
	if !validSlug(p.Slug) {
		return "Use a slug of at most 120 lowercase letters, numbers, and single hyphens."
	}
	if !utf8.ValidString(p.Markdown) || len(p.Markdown) > maxMarkdownBytes {
		return "Enter valid Markdown of at most 256 KiB."
	}
	if !utf8.ValidString(p.Summary) || utf8.RuneCountInString(p.Summary) > 2000 {
		return "Enter a preview description of at most 2,000 characters."
	}
	if p.ChildrenDisplay != "hidden" && p.ChildrenDisplay != "list" && p.ChildrenDisplay != "blocks" {
		return "Choose hidden, preview list, or preview blocks for subpages."
	}
	return ""
}
