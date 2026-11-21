package blog

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// contentError describes a writing error the author can correct.
type contentError string

// Error returns the author-facing validation message.
func (e contentError) Error() string { return string(e) }

// writingFieldError associates a writing failure with its editable field.
type writingFieldError struct {
	error
	field string
}

func (e writingFieldError) Unwrap() error { return e.error }

func validationFields(err error) []string {
	var invalid writingFieldError
	if errors.As(err, &invalid) {
		if component, _, ok := strings.Cut(invalid.field, "."); ok {
			return []string{component, invalid.field}
		}
		return []string{invalid.field}
	}
	return nil
}

// writePost authorizes the actor before validating and saving writing.
func (m *module) writePost(ctx context.Context, p Post) (Post, error) {
	return m.writePostWithSeries(ctx, p, nil, 0)
}

func (m *module) writePostWithSeries(ctx context.Context, p Post, update *seriesUpdate, revisionID int64) (Post, error) {
	p, err := m.authorizePostWrite(ctx, p)
	if err != nil {
		return p, err
	}
	update, err = m.authorizeSeriesUpdate(ctx, update)
	if err != nil {
		return p, err
	}
	return m.persistPostWithSeries(ctx, p, update, revisionID)
}

func (m *module) authorizePostWrite(ctx context.Context, p Post) (Post, error) {
	account := user.GetUserFromContext(ctx)
	permission, err := m.access.Role(ctx, account)
	if err != nil {
		return p, err
	}
	if !permission.valid() {
		return p, errAccessDenied
	}
	if p.ID == "" {
		p.AuthorID = account.ID
	} else {
		current, err := m.posts.GetByID(ctx, p.ID, true)
		if err != nil {
			return p, err
		}
		if permission == rolePoster && current.AuthorID != account.ID {
			return p, errAccessDenied
		}
		p.AuthorID = current.AuthorID
	}
	return p, nil
}

// persistPost validates and saves authorized writing or trusted test fixtures.
// The repository saves content, references, and history atomically.
func (m *module) persistPost(ctx context.Context, p Post) (Post, error) {
	return m.persistPostWithSeries(ctx, p, nil, 0)
}

func (m *module) persistPostWithSeries(ctx context.Context, p Post, update *seriesUpdate, revisionID int64) (Post, error) {
	var err error
	p.Tags, err = normalizeTags(p.Tags)
	if err != nil {
		return p, writingFieldError{err, "tags"}
	}
	generated := p.Slug == ""
	if generated {
		p.Slug = slugFromTitle(p.Title)
	}
	if field, message := validatePostField(p); message != "" {
		return p, writingFieldError{contentError(message), field}
	}
	if p.ID != "" && p.Version < 1 {
		return p, errEditConflict
	}
	if err := m.validateRelatedReferences(ctx, p.Markdown, "post", p.ID, revisionID); err != nil {
		return p, err
	}
	m.mediaMu.RLock()
	defer m.mediaMu.RUnlock()
	base := p.Slug
	for suffix := 2; ; suffix++ {
		refs, err := m.writingMedia(ctx, p.Markdown, "/post/s/"+p.Slug, p.PreviewImage)
		if err != nil {
			return p, err
		}
		var saved Post
		if update == nil {
			saved, err = m.posts.Save(ctx, p, refs)
		} else {
			saved, err = m.posts.SaveWithSeries(ctx, p, refs, update)
		}
		if generated && errors.Is(err, errSlugTaken) {
			p.Slug = numberedSlug(base, suffix)
			continue
		}
		if err == nil {
			m.invalidatePosts()
		}
		return saved, err
	}
}

// writePage validates writing and protects its media from concurrent cleanup.
// The repository enforces page-tree constraints within the save transaction.
func (m *module) writePage(ctx context.Context, p Page, revisionID int64) (Page, error) {
	if err := m.authorizeAdminWrite(ctx); err != nil {
		return p, err
	}
	return m.persistPageRevision(ctx, p, revisionID)
}

// persistPage validates and saves authorized writing or trusted test fixtures.
func (m *module) persistPage(ctx context.Context, p Page) (Page, error) {
	return m.persistPageRevision(ctx, p, 0)
}

func (m *module) persistPageRevision(ctx context.Context, p Page, revisionID int64) (Page, error) {
	var err error
	p.Tags, err = normalizeTags(p.Tags)
	if err != nil {
		return p, writingFieldError{err, "tags"}
	}
	generated := p.Slug == ""
	if generated {
		p.Slug = slugFromTitle(p.Title)
	}
	if field, message := validatePageField(p); message != "" {
		return p, writingFieldError{contentError(message), field}
	}
	if p.ID != "" && p.Version < 1 {
		return p, errEditConflict
	}
	if err := m.validateRelatedReferences(ctx, p.Markdown, "page", p.ID, revisionID); err != nil {
		return p, err
	}
	m.mediaMu.RLock()
	defer m.mediaMu.RUnlock()
	base := p.Slug
	for suffix := 2; ; suffix++ {
		refs, err := m.writingMedia(ctx, p.Markdown, p.URL(), p.PreviewImage)
		if err != nil {
			return p, err
		}
		saved, err := m.pageStore.SavePage(ctx, p, refs)
		if generated && errors.Is(err, errPageSlugTaken) {
			p.Slug = numberedSlug(base, suffix)
			continue
		}
		if err == nil && saved.Version != p.Version {
			// Posts in the feed can link to pages, including descendants of this page.
			m.feed.invalidate()
		}
		return saved, err
	}
}

// writingMedia derives references once for validation and persistence.
// Callers hold mediaMu until the content and its references have been saved.
func (m *module) writingMedia(ctx context.Context, source, path, preview string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if preview != "" {
		image, err := m.images.Open(ctx, preview)
		if errors.Is(err, errNotFound) {
			return nil, writingFieldError{errMissingMedia, "previewImage"}
		}
		if err != nil {
			return nil, err
		}
		image.Content.Close()
	}
	_, components, err := splitPostComponents(source)
	if err != nil {
		return nil, writingFieldError{err, "content"}
	}
	for _, component := range components.Components {
		field := postComponentCatalog.field(component)
		for _, entry := range component.Content.Media() {
			if entry.Bytes == nil {
				continue
			}
			file, err := m.downloads.Open(ctx, entry.ID)
			if errors.Is(err, errNotFound) {
				return nil, writingFieldError{errMissingMedia, field}
			}
			if err != nil {
				return nil, err
			}
			info, err := file.Stat()
			file.Close()
			if err != nil {
				return nil, err
			}
			if info.Size() != *entry.Bytes {
				return nil, writingFieldError{contentError("The file details are out of date. Remove the file and choose it again."), field}
			}
		}
	}
	refs, err := contentMediaReferences(source, path, preview)
	if err != nil {
		return nil, err
	}
	if err := m.checkMediaReferences(ctx, refs); err != nil {
		return nil, writingFieldError{err, "content"}
	}
	if account := user.GetUserFromContext(ctx); account != nil {
		permission, err := m.access.Role(ctx, account)
		if err != nil {
			return nil, err
		}
		if permission == rolePoster {
			for _, ref := range refs {
				id := referencedMediaID(ref)
				if id == "" || !slices.Contains(m.mediaLookupReferences(id), ref) {
					continue
				}
				allowed, err := m.publicMedia(ctx, id)
				if err != nil {
					return nil, err
				}
				if !allowed {
					allowed, err = m.privateMediaForWriter(ctx, id, account.ID)
				}
				if err != nil {
					return nil, err
				}
				if !allowed {
					return nil, writingFieldError{contentError("Choose media you uploaded, already used in your posts, or in public content."), "content"}
				}
			}
		}
	}
	return refs, nil
}

// invalidatePosts expires every public view derived from post storage.
func (m *module) invalidatePosts() {
	m.feed.invalidate()
	m.recentPosts.invalidate()
	m.archivePosts.invalidate()
}
