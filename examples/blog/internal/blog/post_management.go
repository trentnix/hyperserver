package blog

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

// postManagement separates drafts, scheduled posts, and published posts.
type postManagement struct {
	Drafts, Scheduled, Published []postPage
	Query                        string
	Total                        int
}

// managePosts lists all publication states for the administrator.
func (m *module) managePosts(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 240 {
		http.Error(w, "Search must be at most 240 characters.", http.StatusBadRequest)
		return
	}
	var posts []Post
	var err error
	if query == "" {
		posts, err = m.posts.ListMetadata(r.Context())
	} else {
		posts, err = m.posts.SearchPosts(r.Context(), query)
	}
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	history, err := m.history.HistoryOwners(r.Context(), "post")
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	data := postManagement{Query: query, Total: len(posts)}
	now := m.currentTime()
	for _, post := range posts {
		entry := postPage{Post: post, HasHistory: history[post.ID], Scheduled: post.Scheduled(now)}
		if post.Draft {
			data.Drafts = append(data.Drafts, entry)
		} else if entry.Scheduled {
			data.Scheduled = append(data.Scheduled, entry)
		} else {
			data.Published = append(data.Published, entry)
		}
	}
	page := m.views["posts"].Content(r, data)
	page.Title = "Posts"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display posts.", err)
	}
}
