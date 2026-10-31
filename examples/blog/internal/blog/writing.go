package blog

import (
	"context"
	"errors"
)

// contentError describes a writing error the author can correct.
type contentError string

// Error returns the author-facing validation message.
func (e contentError) Error() string { return string(e) }

// writePost applies the same validation, media checks, and invalidation to editor
// saves and history restores. The repository saves content, references, and history atomically.
func (m *module) writePost(ctx context.Context, p Post) (Post, error) {
	var err error
	p.Tags, err = normalizeTags(p.Tags)
	if err != nil {
		return p, err
	}
	generated := p.Slug == ""
	if generated {
		p.Slug = slugFromTitle(p.Title)
	}
	if message := validatePost(p); message != "" {
		return p, contentError(message)
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
		return p, err
	}
	generated := p.Slug == ""
	if generated {
		p.Slug = slugFromTitle(p.Title)
	}
	if message := validatePage(p); message != "" {
		return p, contentError(message)
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
			return nil, errMissingMedia
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
	return refs, m.checkMediaReferences(ctx, refs)
}

// invalidatePosts expires every public view derived from post storage.
func (m *module) invalidatePosts() {
	m.feed.invalidate()
	m.recentPosts.invalidate()
	m.archivePosts.invalidate()
}
