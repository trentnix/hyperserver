package blog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestSeriesSearchWritingAndRanking(t *testing.T) {
	h := newHarness(t)
	ctx := adminWritingContext(t.Context())
	var ids []string
	var bodyPost Post
	for _, entry := range []struct {
		name string
		post Post
	}{
		{"Needle series", Post{Title: "Opening article", Markdown: "Writing"}},
		{"Article matches", Post{Title: "Needle article", Markdown: "Writing"}},
		{"Tag matches", Post{Title: "A tagged article", Tags: "needle", Markdown: "Writing"}},
		{"Summary matches", Post{Title: "An introduction", Summary: "Needle summary", Markdown: "Writing"}},
		{"Body matches", Post{Title: "A hidden draft", Draft: true, Markdown: "A **needle** in the writing."}},
	} {
		post, err := h.blog.persistPost(t.Context(), entry.post)
		if err != nil {
			t.Fatal(err)
		}
		value, err := h.blog.writeSeries(ctx, Series{Title: entry.name}, []string{post.ID})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, value.ID)
		if entry.name == "Body matches" {
			bodyPost = post
		}
	}
	choices, err := h.blog.series.SearchSeries(t.Context(), "NEEDLE", "")
	if err != nil || len(choices) != len(ids) {
		t.Fatal("series search did not match series names and all post information", choices, err)
	}
	for index, choice := range choices {
		if choice.ID != ids[index] || choice.Count != 1 || len(choice.Posts) != 1 {
			t.Fatal("incorrect search rank or member context", choices)
		}
	}
	if choices[4].Posts[0] != bodyPost.Title {
		t.Fatal("a body match did not identify its member post")
	}
	bodyPost.Markdown = "Replacement writing."
	if _, err := h.blog.persistPost(t.Context(), bodyPost); err != nil {
		t.Fatal(err)
	}
	choices, err = h.blog.series.SearchSeries(t.Context(), "needle", "")
	if err != nil || len(choices) != 4 || slices.ContainsFunc(choices, func(choice seriesChoice) bool { return choice.ID == ids[4] }) {
		t.Fatal("series search used an old post revision", err)
	}
	if choices, err := h.blog.series.SearchSeries(t.Context(), "", ""); err != nil || len(choices) != 0 {
		t.Fatal("an empty query preloaded series", err)
	}
}

func TestSeriesSearchPermissionsAndHTTP(t *testing.T) {
	h := usersHarness(t)
	poster := registeredPoster(t, h)
	ctx := context.WithValue(t.Context(), user.UserContextKey, poster)
	own, err := h.blog.persistPost(t.Context(), Post{Title: "An author's draft", Draft: true, Markdown: "Private search phrase", AuthorID: poster.ID})
	if err != nil {
		t.Fatal(err)
	}
	owned, err := h.blog.writeSeries(ctx, Series{Title: "Author collection"}, []string{own.ID})
	if err != nil {
		t.Fatal(err)
	}
	other, mixed := seriesPost(t, h, "Other author's post"), seriesPost(t, h, "Mixed author post")
	admin := adminWritingContext(t.Context())
	if _, err := h.blog.writeSeries(admin, Series{Title: "Private search phrase"}, []string{other.ID}); err != nil {
		t.Fatal(err)
	}
	mixedPost, err := h.blog.persistPost(t.Context(), Post{Title: "Mixed own post", Markdown: "Private search phrase", AuthorID: poster.ID})
	if err != nil {
		t.Fatal(err)
	}
	mixedSeries, err := h.blog.writeSeries(ctx, Series{Title: "Mixed collection"}, []string{mixedPost.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.blog.writeSeries(admin, *mixedSeries, []string{mixedPost.ID, mixed.ID}); err != nil {
		t.Fatal(err)
	}
	w := h.request("GET", "/post/series?q="+url.QueryEscape("Private search phrase"), nil, false)
	requireStatus(t, w, http.StatusOK)
	var choices []seriesChoice
	if err := json.Unmarshal(w.Body.Bytes(), &choices); err != nil || len(choices) != 1 || choices[0].ID != owned.ID || choices[0].Posts[0] != own.Title {
		t.Fatal("series search exposed another author's metadata or an unmanageable series", w.Body.String(), err)
	}
	w = h.request("GET", "/post/series?q=", nil, false)
	requireStatus(t, w, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal("a blank search returned the series inventory")
	}
	for _, query := range []string{strings.Repeat("界", 241), "\xff"} {
		requireStatus(t, h.request("GET", "/post/series?q="+url.QueryEscape(query), nil, false), http.StatusBadRequest)
	}
	requireStatus(t, h.request("GET", "/post/series/"+other.ID+"?q=phrase", nil, false), http.StatusForbidden)
	h.cookies = nil
	if status := h.request("GET", "/post/series?q=phrase", nil, false).Code; status == http.StatusOK {
		t.Fatal("anonymous series search was allowed")
	}
}

func TestSeriesSearchBoundsAndCancellation(t *testing.T) {
	h := newHarness(t)
	for i := range seriesSearchLimit + 3 {
		post := seriesPost(t, h, fmt.Sprintf("Member %d", i))
		if _, err := h.blog.writeSeries(adminWritingContext(t.Context()), Series{Title: fmt.Sprintf("Common series %02d", i)}, []string{post.ID}); err != nil {
			t.Fatal(err)
		}
	}
	choices, err := h.blog.series.SearchSeries(t.Context(), "Common series", "")
	if err != nil || len(choices) != seriesSearchLimit || choices[0].Title != "Common series 00" || choices[len(choices)-1].Title != "Common series 19" {
		t.Fatal("series search was not bounded and deterministic", choices, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := h.blog.series.SearchSeries(ctx, "Common", ""); !errors.Is(err, context.Canceled) {
		t.Fatal("series search ignored cancellation", err)
	}
}

func TestSeriesSelectionCapacity(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	var ids []string
	for i := range maxSeriesPosts - 1 {
		ids = append(ids, seriesPost(t, h, fmt.Sprintf("Series capacity %d", i)).ID)
	}
	ctx := adminWritingContext(t.Context())
	value, err := h.blog.writeSeries(ctx, Series{Title: "Almost full series"}, ids)
	if err != nil {
		t.Fatal(err)
	}
	unassigned := seriesPost(t, h, "Unassigned post")
	paths := []string{"/post/series", "/post/series/" + unassigned.ID}
	for _, path := range paths {
		requireStatus(t, h.request("GET", path+"?collection="+value.ID, nil, false), http.StatusOK)
	}
	last := seriesPost(t, h, "Final series member")
	if _, err := h.blog.writeSeries(ctx, *value, append(ids, last.ID)); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		w := h.request("GET", path+"?collection="+value.ID, nil, false)
		requireStatus(t, w, http.StatusUnprocessableEntity)
		if !strings.Contains(w.Body.String(), "This series already has 100 posts.") {
			t.Fatal("a series filled after searching did not explain its capacity limit", w.Body.String())
		}
	}
	requireStatus(t, h.request("GET", "/post/series/"+last.ID, nil, false), http.StatusOK)
}

func TestSeriesReloadCurrentMembership(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	post := seriesPost(t, h, "Current series member")
	path := "/post/series/" + post.ID
	read := func() (string, int64, string) {
		t.Helper()
		w := h.request("GET", path, nil, false)
		requireStatus(t, w, http.StatusOK)
		var data struct {
			ID      string
			Version int64
			Source  string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data.ID, data.Version, data.Source
	}
	if id, version, source := read(); id != "" || version != 0 || source != "" {
		t.Fatal("an unassigned post did not return empty series state")
	}
	ctx := adminWritingContext(t.Context())
	value, err := h.blog.writeSeries(ctx, Series{Title: "Original name"}, []string{post.ID})
	if err != nil {
		t.Fatal(err)
	}
	value.Title = "Updated name"
	value, err = h.blog.writeSeries(ctx, *value, []string{post.ID})
	if err != nil {
		t.Fatal(err)
	}
	id, version, source := read()
	content, err := parseSeriesContent(source)
	if err != nil || id != value.ID || version != value.Version || content.Title != "Updated name" || !slices.Equal(content.Posts, []string{post.ID}) {
		t.Fatal("reload did not return the current content and version together", id, version, content, err)
	}
	if _, err := h.blog.writeSeries(ctx, *value, []string{}); err != nil {
		t.Fatal(err)
	}
	if id, version, source := read(); id != "" || version != 0 || source != "" {
		t.Fatal("a removed series did not restore unassigned state")
	}
	h.cookies = nil
	if w := h.request("GET", path, nil, false); w.Code == http.StatusOK {
		t.Fatal("anonymous callers could reload series state")
	}
}
