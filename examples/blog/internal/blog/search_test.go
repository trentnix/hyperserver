package blog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// searchRepositoryFunc checks request budgets without depending on query speed.
type searchRepositoryFunc func(context.Context, string) (searchResults, error)

func (f searchRepositoryFunc) SearchPublic(ctx context.Context, query string) (searchResults, error) {
	return f(ctx, query)
}

// TestPublicSearchBudget checks per-client admission before storage work and
// verifies that the budget does not block ordinary reading or another instance.
func TestPublicSearchBudget(t *testing.T) {
	h := newHarness(t)
	var calls int
	h.blog.searchStore = searchRepositoryFunc(func(context.Context, string) (searchResults, error) {
		calls++
		return searchResults{}, nil
	})
	for range searchRequestsPerMinute {
		requireStatus(t, h.request("GET", "/search?q=test", nil, false), http.StatusOK)
	}
	for _, method := range []string{"GET", "HEAD"} {
		w := h.request(method, "/search?q=test", nil, true)
		requireStatus(t, w, http.StatusTooManyRequests)
		if w.Header().Get("Retry-After") == "" || w.Header().Get("Cache-Control") != "no-store" || calls != searchRequestsPerMinute {
			t.Fatal("rejected search accessed storage or omitted retry/cache headers")
		}
	}
	requireStatus(t, h.request("GET", "/posts", nil, false), http.StatusOK)
	r := httptest.NewRequest("GET", "https://blog.example/search?q=test", nil)
	r.RemoteAddr = "192.0.2.2:1234"
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	requireStatus(t, w, http.StatusOK)
	other := newHarness(t)
	requireStatus(t, other.request("GET", "/search?q=test", nil, false), http.StatusOK)
}

// TestPublicSearchDeadline checks the work deadline, early cancellation, and
// timeout responses. An earlier request deadline must win over the search budget.
func TestPublicSearchDeadline(t *testing.T) {
	h := newHarness(t)
	var workContext context.Context
	h.blog.searchStore = searchRepositoryFunc(func(ctx context.Context, _ string) (searchResults, error) {
		workContext = ctx
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > searchTimeout {
			t.Fatal("search has no bounded work deadline")
		}
		return searchResults{}, nil
	})
	requireStatus(t, h.request("GET", "/search?q=test", nil, false), http.StatusOK)
	if !errors.Is(workContext.Err(), context.Canceled) {
		t.Fatal("completed search retained its work context")
	}
	for _, hx := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		wantDeadline, _ := ctx.Deadline()
		h.blog.searchStore = searchRepositoryFunc(func(work context.Context, _ string) (searchResults, error) {
			if deadline, _ := work.Deadline(); !deadline.Equal(wantDeadline) {
				t.Fatal("search extended the request deadline")
			}
			<-work.Done()
			return searchResults{}, work.Err()
		})
		r := httptest.NewRequest("GET", "/search?q=test", nil).WithContext(ctx)
		if hx {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		h.blog.search(w, r)
		requireStatus(t, w, http.StatusServiceUnavailable)
		if w.Header().Get("Retry-After") == "" || strings.Contains(w.Body.String(), "results found") {
			t.Fatal("timeout looked successful or omitted retry guidance")
		}
	}
}

// seedPublicSearch mixes public results with content that readers must not find.
func seedPublicSearch(t *testing.T, h *harness) {
	t.Helper()
	for _, p := range []Post{
		{Slug: "needle-post", Title: "Needle post", Summary: "A public summary"},
		{Slug: "body-match", Title: "Body match", Markdown: "A needle in the body"},
		{Slug: "private-draft", Title: "Needle private draft", Draft: true},
		{Slug: "future-post", Title: "Needle future post", Published: time.Now().Add(24 * time.Hour)},
	} {
		if _, err := h.blog.posts.Save(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []Page{
		{Slug: "summary-match", Title: "Summary match", Summary: "A needle in the summary", Published: true},
		{Slug: "private-page", Title: "Needle private page"},
	} {
		if _, err := h.blog.pageStore.SavePage(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	parent, err := h.blog.pageStore.GetPageBySlug(t.Context(), "private-page", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"private-child", "private-grandchild"} {
		parent, err = h.blog.pageStore.SavePage(t.Context(), Page{Slug: slug, Title: "Needle " + slug, Published: true, ParentID: parent.ID})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestPublicSearchStorage checks visibility, ranking, literal matching, and failures.
func TestPublicSearchStorage(t *testing.T) {
	h := newHarness(t)
	seedPublicSearch(t, h)
	store := h.blog.posts.(*sqliteRepository)
	got, err := store.SearchPublic(t.Context(), " nEeDlE ")
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, item := range got.Items {
		titles = append(titles, item.Title)
	}
	if got.Total != 3 || !reflect.DeepEqual(titles, []string{"Needle post", "Summary match", "Body match"}) {
		t.Fatalf("unexpected public results: %+v", got)
	}
	for _, query := range []string{"absent", "%_", "' OR 1=1 --"} {
		if got, err := store.SearchPublic(t.Context(), query); err != nil || got.Total != 0 {
			t.Fatalf("query %q: %+v, %v", query, got, err)
		}
	}
	p, err := store.Save(t.Context(), Post{Slug: "unicode", Title: "Straße %_"})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"STRASSE", "%_"} {
		if got, err := store.SearchPublic(t.Context(), query); err != nil || got.Total != 1 || got.Items[0].Slug != p.Slug {
			t.Fatalf("query %q: %+v, %v", query, got, err)
		}
	}
	p.Title = "Changed"
	if _, err := store.Save(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if got, err := store.SearchPublic(t.Context(), "STRASSE"); err != nil || got.Total != 0 {
		t.Fatal("search included old post revisions", got, err)
	}
	page, err := store.GetPageBySlug(t.Context(), "summary-match", true)
	if err != nil {
		t.Fatal(err)
	}
	page.Summary = "Changed"
	if _, err := store.SavePage(t.Context(), page); err != nil {
		t.Fatal(err)
	}
	if got, err := store.SearchPublic(t.Context(), "needle"); err != nil || got.Total != 2 {
		t.Fatal("search included old page revisions", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.SearchPublic(ctx, "needle"); !errors.Is(err, context.Canceled) {
		t.Fatal("search ignored cancellation", err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := store.SearchPublic(t.Context(), "needle"); err == nil || got.Total != 0 || len(got.Items) != 0 {
		t.Fatal("search returned success or partial results after a database failure", got, err)
	}
	if got, err := store.SearchPublic(t.Context(), "  "); err != nil || got.Total != 0 {
		t.Fatal("blank query accessed storage", got, err)
	}
}

// TestPublicSearchLimit ensures later title matches displace newer body matches.
func TestPublicSearchLimit(t *testing.T) {
	h := newHarness(t)
	advance := publicationClock(h, time.Now().Add(-24*time.Hour))
	store := h.blog.posts.(*sqliteRepository)
	if _, err := store.Save(t.Context(), Post{Slug: "title-match", Title: "Needle title"}); err != nil {
		t.Fatal(err)
	}
	advance(time.Now().Add(-23 * time.Hour))
	for i := range searchResultLimit + 5 {
		if _, err := store.Save(t.Context(), Post{Slug: fmt.Sprintf("body-%03d", i), Title: "Body", Markdown: "needle"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.SearchPublic(t.Context(), "needle")
	if err != nil || got.Total != searchResultLimit+6 || len(got.Items) != searchResultLimit || got.Items[0].Slug != "title-match" {
		t.Fatal("incorrect result limit or ranking", got, err)
	}
	again, err := store.SearchPublic(t.Context(), "needle")
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal("equal-rank results changed order", err)
	}
	w := h.request("GET", "/search?q=needle", nil, true)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Showing the 50 best matches") {
		t.Fatal("truncated results were not disclosed")
	}
}

// TestPublicSearchPublication checks visibility at publication and after parent changes.
func TestPublicSearchPublication(t *testing.T) {
	h := newHarness(t)
	start := time.Now().Truncate(time.Second)
	advance := publicationClock(h, start)
	store := h.blog.posts.(*sqliteRepository)
	if _, err := store.Save(t.Context(), Post{Slug: "scheduled", Title: "Needle scheduled", Published: start.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	parent, err := store.SavePage(t.Context(), Page{Slug: "parent", Title: "Parent", Published: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavePage(t.Context(), Page{Slug: "child", Title: "Needle child", Published: true, ParentID: parent.ID}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		now  time.Time
		want int
	}{{start, 1}, {start.Add(time.Hour - time.Second), 1}, {start.Add(time.Hour), 2}} {
		advance(tc.now)
		if got, err := store.SearchPublic(t.Context(), "needle"); err != nil || got.Total != tc.want {
			t.Fatal("incorrect publication boundary", got, err)
		}
	}
	parent.Published = false
	if _, err := store.SavePage(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	if got, err := store.SearchPublic(t.Context(), "needle"); err != nil || got.Total != 1 || got.Items[0].Slug != "scheduled" {
		t.Fatal("unpublishing a parent did not hide its child", got, err)
	}
}

// failedPublicSearch makes accidental blank-query searches and hidden errors observable.
type failedPublicSearch struct{}

func (failedPublicSearch) SearchPublic(context.Context, string) (searchResults, error) {
	return searchResults{}, errors.New("private storage failure")
}

// TestPublicSearchHTTP checks anonymous and administrator views and safe rendering.
func TestPublicSearchHTTP(t *testing.T) {
	h := newHarness(t)
	seedPublicSearch(t, h)
	for _, signedIn := range []bool{false, true} {
		if signedIn {
			h.register(t)
			h.verify(t)
			h.login(t)
		}
		for _, hx := range []bool{false, true} {
			w := h.request("GET", "/search?q=needle", nil, hx)
			requireStatus(t, w, http.StatusOK)
			body := w.Body.String()
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Robots-Tag") != "noindex, follow" {
				t.Fatal("search needs no-store and noindex headers")
			}
			if !strings.Contains(body, "3 results found.") || strings.Contains(body, "Needle private") || strings.Contains(body, "Needle future") {
				t.Fatal("incorrect public visibility", body)
			}
			if strings.Contains(body, "<!DOCTYPE") == hx {
				t.Fatal("wrong full-page/fragment response")
			}
			for _, query := range []string{strings.Repeat("é", 241), string([]byte{0xff})} {
				requireStatus(t, h.request("GET", "/search?q="+url.QueryEscape(query), nil, hx), http.StatusBadRequest)
			}
			query := `<script>alert("x")</script>`
			w = h.request("GET", "/search?q="+url.QueryEscape(query), nil, hx)
			requireStatus(t, w, http.StatusOK)
			if strings.Contains(w.Body.String(), query) || !strings.Contains(w.Body.String(), "No matches.") {
				t.Fatal("query was not escaped or empty results were not explained")
			}
		}
	}
	if _, err := h.blog.posts.Save(t.Context(), Post{Slug: "escaping", Title: `<script>needle</script>`, Summary: `<img src=x onerror=alert(1)>`}); err != nil {
		t.Fatal(err)
	}
	w := h.request("GET", "/search?q=needle", nil, true)
	if strings.Contains(w.Body.String(), "<script>needle") || strings.Contains(w.Body.String(), "<img src=x") || !strings.Contains(w.Body.String(), "&lt;script&gt;needle") {
		t.Fatal("result metadata was not escaped")
	}
	h.blog.searchStore = failedPublicSearch{}
	for _, hx := range []bool{false, true} {
		requireStatus(t, h.request("GET", "/search?q=%20%20", nil, hx), http.StatusOK)
		w := h.request("GET", "/search?q=needle", nil, hx)
		requireStatus(t, w, http.StatusInternalServerError)
		if strings.Contains(w.Body.String(), "private storage") || strings.Contains(w.Body.String(), "No matches.") {
			t.Fatal("storage failure leaked or appeared as no matches")
		}
	}
}

// TestReaderSearchOmission checks actual HTML, including errors and history restores.
func TestReaderSearchOmission(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	p, err := h.blog.posts.Save(t.Context(), Post{Slug: "reader-search", Title: "Reader search"})
	if err != nil {
		t.Fatal(err)
	}
	f := mediaUpload(t, h.blog, "search visibility")
	paths := []string{
		"/post/manage", "/pages", "/profile", "/profile/history", "/media", "/media/unused", "/media/" + f.ID,
		"/post/create", "/post/update/" + p.ID, "/post/delete/" + p.ID, "/pages/create", "/pages/update/" + h.projectsID,
		"/history/post/" + p.ID, "/media/delete/" + f.ID,
	}
	request := func(path string, hx, restore bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://blog.example"+path, nil)
		r.Header.Set("HX-Request", fmt.Sprint(hx))
		r.Header.Set("HX-Target", "article-content")
		r.Header.Set("HX-History-Restore-Request", fmt.Sprint(restore))
		for _, cookie := range h.cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		return w
	}
	for _, mode := range []struct{ hx, restore bool }{{}, {hx: true}, {hx: true, restore: true}} {
		for _, path := range paths {
			w := request(path, mode.hx, mode.restore)
			requireStatus(t, w, http.StatusOK)
			if strings.Contains(w.Body.String(), "reader-search-form") || strings.Contains(w.Body.String(), `action="/search"`) {
				t.Fatalf("admin response %s contains reader search", path)
			}
		}
		for _, path := range []string{"/", "/posts", "/search", "/about", "/projects", "/post/s/" + p.Slug} {
			w := request(path, mode.hx, mode.restore)
			requireStatus(t, w, http.StatusOK)
			if !strings.Contains(w.Body.String(), "reader-search-form") {
				t.Fatalf("public response %s omitted reader search for administrator", path)
			}
		}
	}
	h.cookies = nil
	for _, path := range paths {
		w := request(path, false, false)
		requireStatus(t, w, http.StatusForbidden)
		if strings.Contains(w.Body.String(), "reader-search-form") {
			t.Fatal("access denial contains reader search", path)
		}
	}
}

// TestSearchMetadata covers concise Unicode summaries and page route aliases.
func TestSearchMetadata(t *testing.T) {
	if got := searchSummary(" a\n\t b "); got != "a b" {
		t.Fatal(got)
	}
	if got := searchSummary(strings.Repeat("é", 201)); got != strings.Repeat("é", 200)+"…" {
		t.Fatal(got)
	}
	for _, tc := range []struct{ kind, slug, want string }{
		{"post", "about", "/post/s/about"},
		{"page", "about", "/about"},
		{"page", "projects", "/projects"},
		{"page", "guide", "/page/guide"},
	} {
		if got := (searchResult{Kind: tc.kind, Slug: tc.slug}).URL(); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}
