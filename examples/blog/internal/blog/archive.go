package blog

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"
)

const archivePageSize = 30

// archive groups one page of public post links by month without loading bodies.
func (m *module) archive(w http.ResponseWriter, r *http.Request) {
	number := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		var err error
		number, err = strconv.Atoi(raw)
		if err != nil || number < 1 || number-1 > math.MaxInt/archivePageSize {
			http.Error(w, "Page must be a positive whole number.", http.StatusBadRequest)
			return
		}
	}
	posts, err := m.loadArchivePage(r.Context(), number)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	if number > 1 && len(posts) == 0 {
		m.storageError(w, r, errNotFound)
		return
	}
	data := archivePage{Number: number}
	if number > 1 {
		data.Previous = archiveURL(number - 1)
	}
	if len(posts) > archivePageSize {
		data.Next = archiveURL(number + 1)
		posts = posts[:archivePageSize]
	}
	data.Months = groupArchive(posts)

	page := m.views["archive"].Content(r, data)
	page.Title = "All Posts"
	if number > 1 {
		page.Title += " — Page " + strconv.Itoa(number)
	}
	m.setSharing(page, r, sharingPreview{Title: page.Title, URL: archiveURL(number), Type: "website"}, "")
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// archiveURL gives the first page one canonical address, without query parameters.
func archiveURL(number int) string {
	if number == 1 {
		return "/posts"
	}
	return "/posts?page=" + strconv.Itoa(number)
}

// recent displays the configured number of newest links for the sidebar or footer.
func (m *module) recent(w http.ResponseWriter, r *http.Request) {
	posts, err := m.loadRecent(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	page := m.views["recent"].Content(r, posts)
	page.Title = "Recent Posts"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// loadRecent shares the bounded, publication-aware cache between pages and fragments.
func (m *module) loadRecent(ctx context.Context) ([]ArchiveEntry, error) {
	return m.recentPosts.loadUntil(ctx, m.settings.RecentPostCount, func() ([]ArchiveEntry, time.Time, error) {
		next, err := m.posts.NextPublication(ctx)
		if err != nil {
			return nil, time.Time{}, err
		}
		posts, err := m.posts.ListRecent(ctx, m.settings.RecentPostCount)
		return posts, next, err
	})
}

// archivePage contains one bounded page of month-grouped links and adjacent URLs.
type archivePage struct {
	Months         []archiveMonth
	Number         int
	Previous, Next string
}

// archiveMonth contains the heading and post links for one archive month.
type archiveMonth struct {
	Name  string
	Posts []ArchiveEntry
}

// groupArchive groups adjacent entries by publication month, preserving order.
// Posts must already be sorted by publication time.
func groupArchive(posts []ArchiveEntry) []archiveMonth {
	var months []archiveMonth
	for _, post := range posts {
		name := post.Published.Format("January 2006")
		if len(months) == 0 || months[len(months)-1].Name != name {
			months = append(months, archiveMonth{Name: name})
		}
		last := &months[len(months)-1]
		last.Posts = append(last.Posts, post)
	}
	return months
}
