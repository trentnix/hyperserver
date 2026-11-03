package blog

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
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
	Tags                                             string // Canonical comma-separated search labels.
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
	// Literal case-insensitive matches rank title, tags, summary, then Markdown.
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
	// The caller supplies rendered image references for the same content.
	// Unchanged content and settings preserve Version and history.
	Save(context.Context, Post, []string) (Post, error)
	// Delete removes a post or returns errNotFound. Uploaded images remain available.
	Delete(context.Context, string) error
}

// validatePost returns the first user-facing validation error, or an empty string.
func validatePost(p Post) string {
	_, message := validatePostField(p)
	return message
}

// validatePostField identifies the control associated with a validation error.
func validatePostField(p Post) (string, string) {
	if p.Title == "" || !utf8.ValidString(p.Title) || utf8.RuneCountInString(p.Title) > 240 {
		return "title", "Enter a title of at most 240 characters."
	}
	if !utf8.ValidString(p.Summary) || utf8.RuneCountInString(p.Summary) > 2000 {
		return "summary", "Keep the summary under 2,000 characters."
	}
	if strings.TrimSpace(p.Markdown) == "" || len(p.Markdown) > maxMarkdownBytes || !utf8.ValidString(p.Markdown) {
		return "content", "Enter valid Markdown of at most 256 KiB."
	}
	if !validSlug(p.Slug) {
		return "slug", "Use a slug of at most 120 lowercase letters, numbers, and single hyphens."
	}
	return "", ""
}
