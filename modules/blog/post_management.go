package blog

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

// postManagement separates drafts, scheduled posts, and published posts.
type postManagement struct {
	Drafts, Scheduled, Published []postPage
	Query                        string
	Total                        int
}

// managePosts lists all publication states for the current writer.
func (m *module) managePosts(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 240 {
		http.Error(w, "Search must be at most 240 characters.", http.StatusBadRequest)
		return
	}
	data, err := m.postManagement(r.Context(), query)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	page := m.views["posts"].Content(r, data)
	page.Title = "Posts"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display posts.", err)
	}
}

// postManagement loads the same grouped results for navigation and deletion updates.
func (m *module) postManagement(ctx context.Context, query string) (postManagement, error) {
	account := user.GetUserFromContext(ctx)
	permission, err := m.access.Role(ctx, account)
	if err != nil {
		return postManagement{}, err
	}
	if !permission.valid() {
		return postManagement{}, errAccessDenied
	}
	var posts []Post
	if query == "" {
		posts, err = m.posts.ListMetadata(ctx)
	} else {
		authorID := ""
		if permission == rolePoster {
			authorID = account.ID
		}
		posts, err = m.posts.SearchPosts(ctx, query, authorID)
	}
	if err != nil {
		return postManagement{}, err
	}
	history, err := m.history.HistoryOwners(ctx, "post")
	if err != nil {
		return postManagement{}, err
	}
	data := postManagement{Query: query}
	now := m.currentTime()
	for _, post := range posts {
		if permission != roleAdmin && post.AuthorID != account.ID {
			continue
		}
		data.Total++
		entry := postPage{Post: post, HasHistory: history[post.ID], Scheduled: post.Scheduled(now), CanEdit: true}
		if post.Draft {
			data.Drafts = append(data.Drafts, entry)
		} else if entry.Scheduled {
			data.Scheduled = append(data.Scheduled, entry)
		} else {
			data.Published = append(data.Published, entry)
		}
	}
	return data, nil
}
