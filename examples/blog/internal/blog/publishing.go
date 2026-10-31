package blog

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/util"
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

// publish makes a saved post publicly readable and eligible for RSS and archives.
func (m *module) publish(w http.ResponseWriter, r *http.Request) {
	m.setPublication(w, r, true)
}

// unpublish hides a post while retaining its content, URL, and first publication date.
func (m *module) unpublish(w http.ResponseWriter, r *http.Request) {
	m.setPublication(w, r, false)
}

// setPublication invalidates public caches only after storage confirms the change.
func (m *module) setPublication(w http.ResponseWriter, r *http.Request, published bool) {
	post, err := m.posts.SetPublished(r.Context(), r.PathValue("id"), published)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	m.feed.invalidate()
	m.recentPosts.invalidate()
	m.archivePosts.invalidate()
	// Management actions return to the list. Never accept a caller-supplied URL.
	if !published && r.URL.Query().Get("from") == "posts" {
		util.RedirectToURL(w, r, "/post/manage")
		return
	}
	util.RedirectToURL(w, r, "/post/s/"+post.Slug)
}
