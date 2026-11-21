package blog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

func (s *sqliteRepository) PublicPostLinks(ctx context.Context, ids []string) ([]Post, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxRelatedLinks {
		return nil, fmt.Errorf("too many related post IDs")
	}
	args := []any{s.now().Unix()}
	for _, id := range ids {
		args = append(args, id)
	}
	return s.listPosts(ctx, "SELECT id, slug, title, '', '', published, draft, '', version, '', author_id FROM blog_post_current WHERE draft=0 AND published<=? AND id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")", args...)
}

func (s *sqliteRepository) PublicPageLinks(ctx context.Context, ids []string) ([]Page, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxRelatedLinks {
		return nil, fmt.Errorf("too many related page IDs")
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, publishedPages+`SELECT id, slug, title FROM blog_page_current
WHERE id IN (SELECT id FROM visible) AND id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pages []Page
	for rows.Next() {
		var page Page
		if err := rows.Scan(&page.ID, &page.Slug, &page.Title); err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, rows.Err()
}

func (m *module) canSelectRelatedLink(ctx context.Context, id string) (bool, error) {
	if pageID, ok := strings.CutPrefix(id, relatedPagePrefix); ok {
		permission := m.authorizeAdminWrite(ctx)
		if permission != nil && !errors.Is(permission, errAccessDenied) {
			return false, permission
		}
		_, err := m.pageStore.GetPageByID(ctx, pageID, permission == nil)
		if errors.Is(err, errNotFound) {
			return false, nil
		}
		return err == nil, err
	}
	_, err := m.posts.GetByID(ctx, id, false)
	if !errors.Is(err, errNotFound) {
		return err == nil, err
	}
	post, err := m.posts.GetByID(ctx, id, true)
	if errors.Is(err, errNotFound) {
		return false, nil
	}
	return err == nil && m.canEditPost(ctx, post), err
}

type relatedChoice struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state,omitempty"`
	Kind  string `json:"kind,omitempty"`
}

// Choices expose private content only to writers who can edit it.
func (m *module) relatedChoices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	ids := strings.FieldsFunc(r.URL.Query().Get("ids"), func(r rune) bool { return r == ',' })
	exclude := r.URL.Query().Get("exclude")
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 240 || len(ids) > maxRelatedLinks || exclude != "" && !validRelatedLink(exclude) {
		http.Error(w, "Invalid related content search.", http.StatusBadRequest)
		return
	}
	needPosts, needPages := query != "", query != ""
	for _, id := range ids {
		if !validRelatedLink(id) {
			http.Error(w, "Invalid related link.", http.StatusBadRequest)
			return
		}
		if strings.HasPrefix(id, relatedPagePrefix) {
			needPages = true
		} else {
			needPosts = true
		}
	}
	result := struct {
		Choices  []relatedChoice `json:"choices"`
		Selected []relatedChoice `json:"selected"`
	}{Choices: []relatedChoice{}, Selected: []relatedChoice{}}
	var posts []Post
	var pages []relatedChoice
	var err error
	if needPosts {
		posts, err = m.posts.ListMetadata(r.Context())
		if err != nil {
			m.storageError(w, r, err)
			return
		}
	}
	if needPages {
		pages, err = m.relatedPageChoices(r.Context(), m.requestRole(r) == roleAdmin)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
	}
	match := newContentMatcher(query)
	needle := []byte(match.query)
	var exact []relatedChoice
	add := func(choice relatedChoice) {
		if choice.ID == exclude {
			return
		}
		if slices.Contains(ids, choice.ID) {
			result.Selected = append(result.Selected, choice)
		} else if query != "" {
			title := match.normalize(choice.Title)
			if bytes.Equal(title, needle) {
				exact = append(exact, choice)
			} else if bytes.Contains(title, needle) {
				result.Choices = append(result.Choices, choice)
			}
		}
	}
	now := m.currentTime()
	for _, post := range posts {
		state := ""
		if post.Draft || post.Scheduled(now) {
			if !m.canEditPost(r.Context(), post) {
				continue
			}
			state = "Draft"
			if !post.Draft {
				state = "Scheduled"
			}
		}
		add(relatedChoice{ID: post.ID, Title: post.Title, State: state, Kind: "Post"})
	}
	for _, page := range pages {
		add(page)
	}
	slices.SortStableFunc(result.Choices, func(left, right relatedChoice) int {
		return strings.Compare(strings.ToLower(left.Title), strings.ToLower(right.Title))
	})
	if len(exact) > 0 {
		result.Choices = append(exact, result.Choices...)
	}
	result.Choices = result.Choices[:min(len(result.Choices), 30)]
	writeJSON(w, r, http.StatusOK, result)
}

func (m *module) relatedPageChoices(ctx context.Context, admin bool) ([]relatedChoice, error) {
	public, err := m.pageStore.ListPages(ctx, false)
	if err != nil {
		return nil, err
	}
	pages := public
	visible := make(map[string]bool)
	if admin {
		for _, page := range public {
			visible[page.ID] = true
		}
		pages, err = m.pageStore.ListPages(ctx, true)
		if err != nil {
			return nil, err
		}
	}
	choices := make([]relatedChoice, 0, len(pages))
	for _, page := range pages {
		state := ""
		if !page.Published {
			state = "Draft"
		} else if admin && !visible[page.ID] {
			state = "Hidden"
		}
		choices = append(choices, relatedChoice{ID: relatedPagePrefix + page.ID, Title: page.Title, State: state, Kind: "Page"})
	}
	return choices, nil
}
