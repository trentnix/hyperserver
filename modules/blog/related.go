package blog

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

type postLinkReader interface {
	PublicPostLinks(context.Context, []string) ([]Post, error)
}

func (s *sqliteRepository) PublicPostLinks(ctx context.Context, ids []string) ([]Post, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxRelatedPosts {
		return nil, fmt.Errorf("too many related post IDs")
	}
	args := []any{s.now().Unix()}
	for _, id := range ids {
		args = append(args, id)
	}
	return s.listPosts(ctx, "SELECT id, slug, title, '', '', published, draft, '', version, '', author_id FROM blog_post_current WHERE draft=0 AND published<=? AND id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")", args...)
}

type relatedChoice struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state,omitempty"`
}

// Choices include public posts and private posts the writer can edit.
func (m *module) relatedPostChoices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	ids := strings.FieldsFunc(r.URL.Query().Get("ids"), func(r rune) bool { return r == ',' })
	exclude := r.URL.Query().Get("exclude")
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 240 || len(ids) > maxRelatedPosts || exclude != "" && !validRelatedID(exclude) {
		http.Error(w, "Invalid post search.", http.StatusBadRequest)
		return
	}
	for _, id := range ids {
		if !validRelatedID(id) {
			http.Error(w, "Invalid post selection.", http.StatusBadRequest)
			return
		}
	}
	posts, err := m.posts.ListMetadata(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	result := struct {
		Posts    []relatedChoice `json:"posts"`
		Selected []relatedChoice `json:"selected"`
		More     bool            `json:"more"`
	}{Posts: []relatedChoice{}, Selected: []relatedChoice{}}
	match := newContentMatcher(query)
	for _, post := range posts {
		if post.ID == exclude {
			continue
		}
		state := ""
		if post.Draft || post.Scheduled(m.currentTime()) {
			if !m.canEditPost(r.Context(), post) {
				continue
			}
			state = "Draft"
			if !post.Draft {
				state = "Scheduled"
			}
		}
		choice := relatedChoice{ID: post.ID, Title: post.Title, State: state}
		if slices.Contains(ids, post.ID) {
			result.Selected = append(result.Selected, choice)
			continue
		}
		if query == "" || match.rank(post.Title, "", "") < 0 {
			continue
		}
		if len(result.Posts) == 30 {
			result.More = true
			continue
		}
		result.Posts = append(result.Posts, choice)
	}
	writeJSON(w, r, http.StatusOK, result)
}
