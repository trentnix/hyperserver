package blog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestContentMatcher covers literal search and Unicode case folding without a
// database-specific wildcard language.
func TestContentMatcher(t *testing.T) {
	for _, tc := range []struct {
		query, title, summary, body string
		want                        int
	}{
		{"needle", "A NEEDLE", "needle", "needle", 0},
		{"needle", "Other", "A needle", "needle", 1},
		{"needle", "Other", "Other", "A Needle", 2},
		{"needle", "Other", "Other", "Other", -1},
		{"STRASSE", "Straße", "", "", 0},
		{"ÉCOLE", "", "École", "", 1},
		{"%_", "100%_literal", "", "", 0},
		{"%_", "Anything", "", "", -1},
		{"' OR 1=1 --", "Anything", "", "", -1},
	} {
		if got := contentMatcher(tc.query)(tc.title, tc.summary, tc.body); got != tc.want {
			t.Errorf("%q in %q/%q/%q: rank %d, want %d", tc.query, tc.title, tc.summary, tc.body, got, tc.want)
		}
	}
}

// seedContentSearch creates title, summary, and body matches across publication
// states. The matching pages are children of an unrelated draft parent.
func seedContentSearch(t *testing.T, h *harness) {
	t.Helper()
	parent, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "hidden-parent", Title: "Unrelated parent"})
	if err != nil {
		t.Fatal(err)
	}
	for i, entry := range []struct{ title, summary, body string }{
		{"Body result", "", "Needle in the body"},
		{"Summary result", "NEEDLE summary", ""},
		{"Needle title", "Needle summary", "Needle body"},
		{"Other result", "Other summary", "Other body"},
	} {
		published := time.Now().Add(-time.Hour)
		if i == 0 {
			published = time.Now().Add(time.Hour)
		}
		_, err := h.blog.posts.Save(t.Context(), Post{Slug: fmt.Sprintf("search-%d", i), Title: entry.title, Summary: entry.summary, Markdown: entry.body, Draft: i == 1, Published: published})
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.blog.pageStore.SavePage(t.Context(), Page{Slug: fmt.Sprintf("search-%d", i), Title: entry.title, Summary: entry.summary, Markdown: entry.body, Published: i != 1, ParentID: parent.ID, DisplayOrder: new(i)})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestContentSearchStorage checks ranking, current-content reads, metadata-only
// results, cancellation, and storage errors for both repositories.
func TestContentSearchStorage(t *testing.T) {
	h := newHarness(t)
	seedContentSearch(t, h)
	want := []string{"Needle title", "Summary result", "Body result"}
	posts, err := h.blog.posts.SearchPosts(t.Context(), "nEeDlE")
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, p := range posts {
		titles = append(titles, p.Title)
		if p.Summary != "" || p.Markdown != "" {
			t.Fatal("post search retained bodies")
		}
	}
	if !reflect.DeepEqual(titles, want) {
		t.Fatalf("post titles = %v, want %v", titles, want)
	}
	pages, err := h.blog.pageStore.SearchPages(t.Context(), "nEeDlE")
	if err != nil {
		t.Fatal(err)
	}
	titles = nil
	for _, p := range pages {
		titles = append(titles, p.Title)
		if p.Markdown != "" {
			t.Fatal("page search retained bodies")
		}
	}
	if !reflect.DeepEqual(titles, want) {
		t.Fatalf("page titles = %v, want %v", titles, want)
	}

	// Old revisions must not keep edited content in the search results.
	p, err := h.blog.posts.GetByID(t.Context(), posts[0].ID, true)
	if err != nil {
		t.Fatal(err)
	}
	p.Title, p.Summary, p.Markdown = "Changed title", "", ""
	if _, err := h.blog.posts.Save(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	page, err := h.blog.pageStore.GetPageByID(t.Context(), pages[0].ID, true)
	if err != nil {
		t.Fatal(err)
	}
	page.Title, page.Summary, page.Markdown = "Changed title", "", ""
	if _, err := h.blog.pageStore.SavePage(t.Context(), page); err != nil {
		t.Fatal(err)
	}
	if matches, err := h.blog.posts.SearchPosts(t.Context(), "needle"); err != nil || len(matches) != 2 {
		t.Fatal("post search retained historical content", matches, err)
	}
	if matches, err := h.blog.pageStore.SearchPages(t.Context(), "needle"); err != nil || len(matches) != 2 {
		t.Fatal("page search retained historical content", matches, err)
	}
	for _, query := range []string{"absent", "%_", "' OR 1=1 --"} {
		if matches, err := h.blog.posts.SearchPosts(t.Context(), query); err != nil || len(matches) != 0 {
			t.Fatal("unexpected post match", query, matches, err)
		}
		if matches, err := h.blog.pageStore.SearchPages(t.Context(), query); err != nil || len(matches) != 0 {
			t.Fatal("unexpected page match", query, matches, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := h.blog.posts.SearchPosts(ctx, "needle"); !errors.Is(err, context.Canceled) {
		t.Fatal("post search ignored cancellation", err)
	}
	if _, err := h.blog.pageStore.SearchPages(ctx, "needle"); !errors.Is(err, context.Canceled) {
		t.Fatal("page search ignored cancellation", err)
	}
	store := h.blog.posts.(*sqliteRepository)
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SearchPosts(t.Context(), "needle"); err == nil {
		t.Fatal("post search hid storage failure")
	}
	if _, err := store.SearchPages(t.Context(), "needle"); err == nil {
		t.Fatal("page search hid storage failure")
	}
}

// TestManagementSearch exercises ordinary and HTMX requests, authorization,
// escaping, validation, and restoration of the normal lists.
func TestManagementSearch(t *testing.T) {
	h := newHarness(t)
	seedContentSearch(t, h)
	for _, path := range []string{"/post/manage", "/pages"} {
		w := h.request("GET", path+"?q=needle", nil, false)
		if w.Code == http.StatusOK || strings.Contains(w.Body.String(), `data-content-list=`) || strings.Contains(w.Body.String(), "Summary result") {
			t.Fatal("anonymous search exposed content")
		}
	}
	h.register(t)
	h.login(t)
	for _, path := range []string{"/post/manage", "/pages"} {
		w := h.request("GET", path+"?q=needle", nil, false)
		if strings.Contains(w.Body.String(), `data-content-list=`) || strings.Contains(w.Body.String(), "Summary result") {
			t.Fatal("unverified account searched content")
		}
	}
	h.verify(t)
	for _, path := range []string{"/post/manage", "/pages"} {
		for _, hx := range []bool{false, true} {
			w := h.request("GET", path+"?q=%20nEeDlE%20", nil, hx)
			requireStatus(t, w, http.StatusOK)
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("search response is cacheable")
			}
			_, body, found := strings.Cut(w.Body.String(), `<section class="info-page page-manager"`)
			if !found {
				t.Fatal("missing management results")
			}
			body, _, _ = strings.Cut(body, "</article>")
			last := -1
			titles := []string{"Needle title", "Summary result", "Body result"}
			if path == "/post/manage" {
				titles = []string{"Summary result", "Body result", "Needle title"}
			}
			for _, title := range titles {
				at := strings.Index(body, ">"+title+"</a>")
				if at <= last {
					t.Fatalf("missing or incorrectly ranked %q: %s", title, body)
				}
				last = at
			}
			if strings.Contains(body, "Other result") || strings.Contains(body, "Unrelated parent") || !strings.Contains(body, `value="nEeDlE"`) || !strings.Contains(body, "Clear search") {
				t.Fatal("search did not filter and preserve query")
			}
			for _, query := range []string{"absent", `<script>alert("x")</script>`} {
				w := h.request("GET", path+"?q="+url.QueryEscape(query), nil, hx)
				requireStatus(t, w, http.StatusOK)
				if !strings.Contains(w.Body.String(), "match your search") || strings.Contains(w.Body.String(), "<script>alert") {
					t.Fatal("incorrect or unescaped empty results")
				}
			}
			w = h.request("GET", path+"?q=%20%20", nil, hx)
			requireStatus(t, w, http.StatusOK)
			if !strings.Contains(w.Body.String(), "Other result") || strings.Contains(w.Body.String(), "Clear search") {
				t.Fatal("blank search did not restore normal listing")
			}
			body = w.Body.String()
			count := strings.Count(body, `data-content-id="`)
			if count == 0 || !strings.Contains(body, fmt.Sprintf(`data-result-count="%d"`, count)) {
				t.Fatal("normal listing must show the total count, including subpages")
			}
			for _, query := range []string{strings.Repeat("é", 241), string([]byte{0xff})} {
				requireStatus(t, h.request("GET", path+"?q="+url.QueryEscape(query), nil, hx), http.StatusBadRequest)
			}
		}
	}
	h.blog.posts = failedContentSearch{h.blog.posts}
	h.blog.pageStore = unavailablePages{}
	for _, path := range []string{"/post/manage", "/pages"} {
		for _, hx := range []bool{false, true} {
			w := h.request("GET", path+"?q=needle", nil, hx)
			requireStatus(t, w, http.StatusInternalServerError)
			if strings.Contains(w.Body.String(), "private storage") || strings.Contains(w.Body.String(), "match your search") {
				t.Fatal("storage failure was leaked or treated as an empty search")
			}
		}
	}
}

// TestPostSearchGroups keeps publication groups while ranking matches inside
// each group and omitting groups with no matching posts.
func TestPostSearchGroups(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	states := []string{"draft", "scheduled", "published"}
	for _, state := range states {
		for _, field := range []string{"body", "summary", "title"} {
			p := Post{Slug: state + "-" + field, Title: state + "-" + field, Draft: state == "draft", Published: time.Now().Add(-time.Hour)}
			if state == "scheduled" {
				p.Published = time.Now().Add(time.Hour)
			}
			switch field {
			case "body":
				p.Markdown = "needle"
			case "summary":
				p.Summary = "needle"
			case "title":
				p.Title = "needle " + p.Title
			}
			if _, err := h.blog.posts.Save(t.Context(), p); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, hx := range []bool{false, true} {
		for _, query := range []string{"", "needle", "draft", "scheduled", "published", "absent"} {
			w := h.request("GET", "/post/manage?q="+query, nil, hx)
			requireStatus(t, w, http.StatusOK)
			for _, state := range states {
				_, section, found := strings.Cut(w.Body.String(), `id="`+state+`-posts"`)
				section, _, _ = strings.Cut(section, "</section>")
				want := query == "" || query == "needle" || query == state
				if found != want {
					t.Fatalf("query %q: %s section present = %t, want %t", query, state, found, want)
				}
				if query == "needle" {
					last := -1
					for _, field := range []string{"title", "summary", "body"} {
						at := strings.Index(section, `href="/post/s/`+state+"-"+field+`"`)
						if at <= last {
							t.Fatalf("%s results are not ranked by title, summary, then body", state)
						}
						last = at
					}
				}
			}
			if query == "absent" && strings.Count(w.Body.String(), "No posts match your search.") != 1 {
				t.Fatal("empty search must have one shared empty state")
			}
			if query == "needle" && !strings.Contains(w.Body.String(), "9 posts found.") {
				t.Fatal("search count must include every group")
			}
		}
	}
}

// failedContentSearch keeps the normal repository available for layout rendering.
type failedContentSearch struct{ repository }

func (failedContentSearch) SearchPosts(context.Context, string) ([]Post, error) {
	return nil, errors.New("private storage failure")
}
