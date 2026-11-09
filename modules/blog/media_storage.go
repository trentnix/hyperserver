package blog

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// mediaReferenceRepository includes private content and history. Without IDs,
// ReferencedImages returns all usage. Otherwise it returns only those uploads.
type mediaReferenceRepository interface {
	ReferencedImages(context.Context, ...string) (map[string]mediaUsage, error)
}

// mediaUsage distinguishes protected current content from removable historical uses.
type mediaUsage struct {
	Current bool
	Content []mediaReference
	History []mediaRevision
}

// mediaReference identifies saved content that currently uses an upload.
type mediaReference struct {
	URL, Title string
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

// ReferencedImages filters indexed URLs before reading content metadata. It never
// reads historical Markdown. Profile URLs still need Go normalization because
// they can retain queries, fragments, and escaped paths.
func (s *sqliteRepository) ReferencedImages(ctx context.Context, ids ...string) (map[string]mediaUsage, error) {
	prefix, args := mediaReferenceSelection(ids)
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	selected := func(id string) bool { return id != "" && (len(ids) == 0 || wanted[id]) }
	used := make(map[string]mediaUsage)
	rows, err := s.db.QueryContext(ctx, prefix+`SELECT i.url, '/post/update/' || p.id AS location, p.title FROM post_images i JOIN blog_post_current p ON p.version_id=i.version_id
UNION SELECT i.url, '/pages/update/' || p.id, p.title FROM page_images i JOIN blog_page_current p ON p.version_id=i.version_id
UNION SELECT image_url, '/profile', 'Site portrait' FROM blog_profile_current
UNION SELECT favicon_url, '/profile', 'Site favicon' FROM blog_profile_current
UNION SELECT background_url, '/profile', 'Site background' FROM blog_profile_current
ORDER BY location, title, url`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var reference string
		var content mediaReference
		if err := rows.Scan(&reference, &content.URL, &content.Title); err != nil {
			rows.Close()
			return nil, err
		}
		if id := referencedImageID(reference); selected(id) {
			usage := used[id]
			usage.Current = true
			if !slices.Contains(usage.Content, content) {
				usage.Content = append(usage.Content, content)
			}
			used[id] = usage
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	// Keep each version's references together so equivalent URLs can be
	// deduplicated against the last entry, not the entire accumulated history.
	rows, err = s.db.QueryContext(ctx, prefix+`SELECT i.url, 'post' AS kind, r.owner_id, r.id AS revision_id, r.title, r.recorded
FROM blog_posts r JOIN post_images i ON i.version_id=r.id
UNION ALL
SELECT i.url, 'page', r.owner_id, r.id, r.title, r.recorded
FROM blog_pages r JOIN page_images i ON i.version_id=r.id
ORDER BY recorded DESC, kind, owner_id, revision_id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var reference, kind, owner string
		var revision mediaRevision
		var number, recorded int64
		if err := rows.Scan(&reference, &kind, &owner, &number, &revision.Title, &recorded); err != nil {
			return nil, err
		}
		revision.URL = "/history/" + kind + "/" + url.PathEscape(owner) + "?revision=" + strconv.FormatInt(number, 10)
		revision.RecordedAt = time.Unix(0, recorded).UTC()
		if id := referencedImageID(reference); selected(id) {
			usage := used[id]
			if len(usage.History) == 0 || usage.History[len(usage.History)-1] != revision {
				usage.History = append(usage.History, revision)
				used[id] = usage
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	rows, err = s.db.QueryContext(ctx, "SELECT id, name, image_url, favicon_url, background_url, recorded FROM blog_profile ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var number, recorded int64
		var name, portrait, favicon, background string
		if err := rows.Scan(&number, &name, &portrait, &favicon, &background, &recorded); err != nil {
			return nil, err
		}
		revision := mediaRevision{URL: "/profile/history?revision=" + strconv.FormatInt(number, 10), Title: "Site Profile — " + name, RecordedAt: time.Unix(0, recorded).UTC()}
		seen := make(map[string]bool)
		for _, ref := range []string{portrait, favicon, background} {
			if id := referencedImageID(ref); selected(id) && !seen[id] {
				usage := used[id]
				usage.History = append(usage.History, revision)
				used[id] = usage
				seen[id] = true
			}
		}
	}
	for _, usage := range used {
		slices.SortStableFunc(usage.History, func(a, b mediaRevision) int { return b.RecordedAt.Compare(a.RecordedAt) })
	}
	return used, rows.Err()
}

// Indexed URLs are normalized but retain their host. Matching the ID suffix
// keeps old-host references without decoding unrelated titles and revisions.
// SQLite still scans URL indexes. Avoiding that scan needs a stored image ID.
func mediaReferenceSelection(ids []string) (string, []any) {
	prefix, filter := "WITH ", ""
	var args []any
	if len(ids) > 0 {
		var lengths []int
		for _, id := range ids {
			args = append(args, id)
			if !slices.Contains(lengths, len(id)) {
				lengths = append(lengths, len(id))
			}
		}
		prefix += "media_ids(id) AS (VALUES " + strings.TrimSuffix(strings.Repeat("(?),", len(ids)), ",") + "), "
		var conditions []string
		for _, length := range lengths {
			conditions = append(conditions, fmt.Sprintf("substr(url, -%d) IN (SELECT id FROM media_ids)", length))
		}
		filter = " WHERE " + strings.Join(conditions, " OR ")
	}
	prefix += "post_images AS (SELECT version_id, url FROM blog_post_images" + filter + "), " +
		"page_images AS (SELECT version_id, url FROM blog_page_images" + filter + ") "
	return prefix, args
}
