package blog

import (
	"context"
	"errors"
	"slices"

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
		return []string{invalid.field}
	}
	return nil
}

// writePost applies the same validation, media checks, and invalidation to editor
// saves and history restores. The repository saves content, references, and history atomically.
func (m *module) writePost(ctx context.Context, p Post) (Post, error) {
	account := user.GetUserFromContext(ctx)
	permission, err := m.access.Role(ctx, account)
	if err != nil {
		return p, err
	}
	if account != nil && !permission.valid() {
		return p, errAccessDenied
	}
	if permission == rolePoster {
		p.AuthorID = account.ID
		if p.ID != "" {
			current, err := m.posts.GetByID(ctx, p.ID, true)
			if err != nil {
				return p, err
			}
			if current.AuthorID != account.ID {
				return p, errAccessDenied
			}
		}
	}
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
	m.mediaMu.RLock()
	defer m.mediaMu.RUnlock()
	base := p.Slug
	for suffix := 2; ; suffix++ {
		refs, err := m.writingImages(ctx, p.Markdown, "/post/s/"+p.Slug, p.PreviewImage)
		if err != nil {
			return p, err
		}
		saved, err := m.posts.Save(ctx, p, refs)
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
func (m *module) writePage(ctx context.Context, p Page) (Page, error) {
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
	m.mediaMu.RLock()
	defer m.mediaMu.RUnlock()
	base := p.Slug
	for suffix := 2; ; suffix++ {
		refs, err := m.writingImages(ctx, p.Markdown, p.URL(), p.PreviewImage)
		if err != nil {
			return p, err
		}
		saved, err := m.pageStore.SavePage(ctx, p, refs)
		if generated && errors.Is(err, errPageSlugTaken) {
			p.Slug = numberedSlug(base, suffix)
			continue
		}
		return saved, err
	}
}

// writingImages derives references once for validation and persistence.
// Callers hold mediaMu until the content and its references have been saved.
func (m *module) writingImages(ctx context.Context, source, path, preview string) ([]string, error) {
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
	refs, err := imageReferences(source, path, preview)
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
				id := referencedImageID(ref)
				if id == "" || !slices.Contains(m.imageLookupReferences(id), ref) {
					continue
				}
				allowed, err := m.publicImage(ctx, id)
				if err != nil {
					return nil, err
				}
				if !allowed {
					allowed, err = m.privateImageForWriter(ctx, id, account.ID)
				}
				if err != nil {
					return nil, err
				}
				if !allowed {
					return nil, writingFieldError{contentError("Choose an image you uploaded, already used in your posts, or in public content."), "content"}
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
