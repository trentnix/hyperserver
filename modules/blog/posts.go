// Package blog provides a server-rendered Markdown blog with an authenticated editor.
// Applications supply storage, authentication middleware, and an admin allowlist.
//
// Use Module to configure the discovered module and ConfigureRendering to select
// its page layouts. Posts are public as soon as they are created. The module has
// no draft state and does not open or close application-owned services.
package blog

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const (
	// MaxContentBytes bounds a post's Markdown source in UTF-8 bytes.
	MaxContentBytes = 256 * 1024
	// MaxImageBytes bounds an encoded image upload.
	MaxImageBytes = 5 * 1024 * 1024
	// PageSize is the number of archive entries per page.
	PageSize = 20
	// MaxListSize bounds a repository List call and the RSS feed.
	MaxListSize = 100
)

var (
	// ErrNotFound means the requested post or image does not exist.
	ErrNotFound = errors.New("blog record not found")
	// ErrConflict means a write would replace an existing ID or act on a post
	// that is missing or whose stored version no longer matches the submitted version.
	ErrConflict = errors.New("the post changed or was deleted; reload before trying again")
	// ErrSlugExists means another post already owns the requested slug.
	ErrSlugExists = errors.New("another post already uses this slug")
	// ErrInvalidPost means a post fails Validate or has an invalid initial version.
	ErrInvalidPost = errors.New("invalid blog post")
)

// Post contains Markdown source, not trusted HTML. PublishedAt remains unchanged on edit.
type Post struct {
	// ID is a UUID that remains stable when the title or slug changes.
	ID string
	// Slug is the unique URL segment, in the form produced by Slug, up to 160 bytes.
	Slug string
	// Title is nonblank UTF-8 text of at most 200 Unicode code points.
	Title string
	// Summary is optional UTF-8 plain text used in RSS, up to 1,000 Unicode code points.
	Summary string
	// Markdown is nonblank UTF-8 source bounded by MaxContentBytes.
	Markdown string
	// PublishedAt is a nonzero publication time that must round-trip through Unix nanoseconds.
	PublishedAt time.Time
	// Version starts at 1 and increases on each update to detect concurrent edits.
	Version int64
}

// Repository stores published posts. Calls must honor ctx and support concurrent use.
// Methods must not mutate their inputs or close application-owned resources.
type Repository interface {
	// List returns posts ordered by PublishedAt descending, then ID ascending.
	// The integer arguments are offset and limit. Offset must be nonnegative,
	// and limit must be between 1 and MaxListSize. An empty page is not an error.
	List(context.Context, int, int) ([]Post, error)
	// GetByID returns the post with the given ID, or ErrNotFound.
	GetByID(context.Context, string) (Post, error)
	// GetBySlug returns the post with the given slug, or ErrNotFound.
	GetBySlug(context.Context, string) (Post, error)
	// Create validates and inserts a post at version 1. It returns ErrConflict
	// for an existing ID and ErrSlugExists for an existing slug.
	// If both conflict, either error can be returned.
	Create(context.Context, Post) error
	// Update validates and replaces editable fields, preserving ID and PublishedAt.
	// It must atomically match Version and increment the stored version.
	// A missing post or version mismatch returns ErrConflict. A slug owned by
	// another post returns ErrSlugExists.
	Update(context.Context, Post) error
	// Delete atomically removes the post with the given ID and version.
	// A missing post or version mismatch returns ErrConflict. Images remain intact.
	Delete(context.Context, string, int64) error
}

// Image stores a validated raster image with an opaque ID, never a user-supplied path.
type Image struct {
	// ID is a UUID used in the public image URL.
	ID string
	// MediaType is image/png, image/jpeg, or image/gif, determined from the data.
	MediaType string
	// Data contains the encoded image, not decoded pixels, bounded by MaxImageBytes.
	Data []byte
}

// ImageStore stores immutable images independently of posts.
// Calls must honor ctx, support concurrent use, and leave resource cleanup to the owner.
// Deleting or editing posts must not remove images used by other posts or open editors.
type ImageStore interface {
	// Save atomically publishes an image whose encoded content the caller has
	// validated. An existing ID must be rejected, never overwritten.
	Save(context.Context, Image) error
	// Get returns an image with caller-owned Data, or ErrNotFound when absent.
	Get(context.Context, string) (Image, error)
}

// Slug converts a title to lowercase ASCII letters and digits, replacing runs of
// other characters with a single hyphen and omitting leading and trailing hyphens.
// It does not truncate the result or ensure uniqueness. If the result is empty,
// the post needs an explicitly entered slug.
func Slug(title string) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if separator && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			separator = false
		} else {
			separator = true
		}
	}
	return b.String()
}

// Validate returns ErrInvalidPost if a post violates the field requirements in Post.
// It does not change the post or check stored IDs, slugs, or versions for conflicts.
func Validate(p Post) error {
	if _, err := uuid.Parse(p.ID); err != nil {
		return ErrInvalidPost
	}
	if p.Slug == "" || len(p.Slug) > 160 || Slug(p.Slug) != p.Slug ||
		strings.TrimSpace(p.Title) == "" || utf8.RuneCountInString(p.Title) > 200 ||
		utf8.RuneCountInString(p.Summary) > 1000 || strings.TrimSpace(p.Markdown) == "" ||
		len(p.Markdown) > MaxContentBytes || !utf8.ValidString(p.Title) ||
		!utf8.ValidString(p.Summary) || !utf8.ValidString(p.Markdown) ||
		p.PublishedAt.IsZero() || !time.Unix(0, p.PublishedAt.UnixNano()).Equal(p.PublishedAt) ||
		p.Version < 1 {
		return ErrInvalidPost
	}
	return nil
}

// RenderMarkdown converts bounded Markdown with raw HTML and dangerous URLs disabled.
// Only Goldmark's escaped output crosses the template.HTML trust boundary.
// Sources larger than MaxContentBytes return ErrInvalidPost. Relative links remain
// relative to the page that displays the result.
func RenderMarkdown(source string) (template.HTML, error) {
	return renderMarkdown(source, nil)
}

// A feed needs absolute destinations because readers display it outside the site.
// Rewrite Markdown nodes before rendering so Goldmark still checks unsafe URLs.
func renderMarkdown(source string, base *url.URL) (template.HTML, error) {
	if len(source) > MaxContentBytes {
		return "", ErrInvalidPost
	}
	markdown := goldmark.New()
	input := []byte(source)
	document := markdown.Parser().Parse(text.NewReader(input))
	if base != nil {
		_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			var destination *[]byte
			switch node := node.(type) {
			case *ast.Link:
				destination = &node.Destination
			case *ast.Image:
				destination = &node.Destination
			}
			if destination != nil {
				if reference, err := url.Parse(string(*destination)); err == nil {
					*destination = []byte(base.ResolveReference(reference).String())
				}
			}
			return ast.WalkContinue, nil
		})
	}
	var output bytes.Buffer
	if err := markdown.Renderer().Render(&output, input, document); err != nil {
		return "", err
	}
	return template.HTML(output.String()), nil
}
