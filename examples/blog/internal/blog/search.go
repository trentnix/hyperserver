package blog

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	searchResultLimit       = 50
	searchTimeout           = 2 * time.Second
	searchRequestsPerMinute = 30
)

// publicSearchRepository searches current public content, never drafts or revisions.
// Title matches precede summary matches, then Markdown matches. Within each rank,
// newer content comes first. Results retain at most searchResultLimit entries.
type publicSearchRepository interface {
	SearchPublic(context.Context, string) (searchResults, error)
}

// searchResult contains display metadata only, without the searched Markdown body.
type searchResult struct {
	Kind, Slug, Title, Summary string
}

// URL uses the same public routes as post and page views, including page aliases.
func (r searchResult) URL() string {
	if r.Kind == "post" {
		return "/post/s/" + r.Slug
	}
	return (Page{Slug: r.Slug}).URL()
}

// searchResults distinguishes the complete match count from the bounded result list.
type searchResults struct {
	Items []searchResult
	Total int
}

// searchPage keeps the query with the results for ordinary and HTMX navigation.
type searchPage struct {
	Query string
	searchResults
}

// search renders public results without caching user queries or account-specific HTML.
func (m *module) search(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, follow")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 240 {
		http.Error(w, "Search must be at most 240 characters.", http.StatusBadRequest)
		return
	}
	data := searchPage{Query: query}
	if query != "" {
		ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
		results, err := m.searchStore.SearchPublic(ctx, query)
		if err == nil {
			err = ctx.Err()
		}
		cancel()
		if errors.Is(err, context.DeadlineExceeded) {
			w.Header().Set("Retry-After", "2")
			http.Error(w, "Search is temporarily unavailable. Please try again.", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		data.searchResults = results
	}
	page := m.views["search"].Content(r, data)
	page.Title = "Search"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display search results.", err)
	}
}
