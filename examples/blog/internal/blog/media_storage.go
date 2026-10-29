package blog

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// mediaReferenceRepository includes private content and history, not just public references.
type mediaReferenceRepository interface {
	ReferencedImages(context.Context) (map[string]mediaUsage, error)
}

// mediaUsage distinguishes protected current content from removable historical uses.
type mediaUsage struct {
	Current bool
	History []mediaRevision
}

// mediaRevision links an upload to the saved writing that would lose it.
type mediaRevision struct {
	URL, Title string
	RecordedAt time.Time
}

// referencedImageID conservatively retains matching uploads even for absolute URLs
// on an old host. Cleanup must not mistake a domain change for an unused image.
func referencedImageID(reference string) string {
	u, err := url.Parse(imageReference(reference, "/"))
	if err != nil {
		return ""
	}
	id := strings.TrimPrefix(u.Path, "/images/")
	if !strings.HasPrefix(u.Path, "/images/") || !validImageID(id) {
		return ""
	}
	return id
}

// ReferencedImages reads current content, profile URLs, and every saved version.
// Historical relative URLs resolve against their owner's current URL, as in the editor.
func (s *sqliteRepository) ReferencedImages(ctx context.Context) (map[string]mediaUsage, error) {
	used := make(map[string]mediaUsage)
	rows, err := s.db.QueryContext(ctx, `SELECT url FROM blog_post_images UNION SELECT url FROM blog_page_images
UNION SELECT image_url FROM blog_profile UNION SELECT favicon_url FROM blog_profile UNION SELECT background_url FROM blog_profile`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var reference string
		if err := rows.Scan(&reference); err != nil {
			rows.Close()
			return nil, err
		}
		if id := referencedImageID(reference); id != "" {
			used[id] = mediaUsage{Current: true}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	rows, err = s.db.QueryContext(ctx, `SELECT r.markdown, r.preview_image, '/post/s/' || p.slug, 'post' AS kind, r.owner_id, r.id AS revision_id, r.title, r.recorded
FROM blog_post_revisions r JOIN blog_posts p ON p.id=r.owner_id
UNION ALL
SELECT r.markdown, r.preview_image, CASE p.slug WHEN 'about' THEN '/about' WHEN 'projects' THEN '/projects' ELSE '/page/' || p.slug END, 'page', r.owner_id, r.id, r.title, r.recorded
FROM blog_page_revisions r JOIN blog_pages p ON p.id=r.owner_id
ORDER BY recorded DESC, kind, owner_id, revision_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var source, preview, contentPath, kind, owner string
		var revision mediaRevision
		var number, recorded int64
		if err := rows.Scan(&source, &preview, &contentPath, &kind, &owner, &number, &revision.Title, &recorded); err != nil {
			return nil, err
		}
		refs, err := imageReferences(source, contentPath, preview)
		if err != nil {
			return nil, err
		}
		revision.URL = "/history/" + kind + "/" + url.PathEscape(owner) + "?revision=" + strconv.FormatInt(number, 10)
		revision.RecordedAt = time.Unix(0, recorded).UTC()
		seen := make(map[string]bool)
		for _, ref := range refs {
			if id := referencedImageID(ref); id != "" && !seen[id] {
				usage := used[id]
				usage.History = append(usage.History, revision)
				used[id] = usage
				seen[id] = true
			}
		}
	}
	return used, rows.Err()
}
