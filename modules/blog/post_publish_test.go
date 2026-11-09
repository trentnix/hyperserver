package blog

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPublishDraftPreview(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(strconv.FormatBool(hx), func(t *testing.T) {
			h := usersHarness(t)
			p, err := h.blog.persistPost(t.Context(), Post{Title: "Preview draft", Summary: "Summary", Markdown: "Stored writing.", Tags: "example", Draft: true})
			if err != nil {
				t.Fatal(err)
			}
			path := "/post/publish/" + p.ID
			preview := h.request("GET", "/post/s/"+p.Slug, nil, hx)
			requireStatus(t, preview, http.StatusOK)
			if !strings.Contains(preview.Body.String(), `action="`+path+`"`) || !strings.Contains(preview.Body.String(), `name="version" value="1"`) {
				t.Fatal("draft preview lacks a versioned publish form")
			}
			// Prime public caches before publishing.
			h.request("GET", "/posts", nil, false)
			h.request("GET", "/posts/recent", nil, false)
			h.request("GET", "/rss", nil, false)
			response := h.request("POST", path, url.Values{"version": {"1"}, "title": {"Injected title"}, "content": {"Injected body"}}, hx)
			status, header := http.StatusSeeOther, "Location"
			if hx {
				status, header = http.StatusOK, "HX-Redirect"
			}
			requireStatus(t, response, status)
			if response.Header().Get(header) != "/post/s/"+p.Slug {
				t.Fatal("publish did not redirect to the post")
			}
			published, err := h.blog.posts.GetByID(t.Context(), p.ID, false)
			if err != nil || published.Draft || published.Published.IsZero() || published.Version != p.Version+1 {
				t.Fatalf("draft was not published: %+v, %v", published, err)
			}
			if published.Title != p.Title || published.Markdown != p.Markdown || published.Summary != p.Summary || published.Tags != p.Tags || published.AuthorID != p.AuthorID {
				t.Fatal("publishing changed stored writing or ownership")
			}
			requireStatus(t, h.request("POST", path, url.Values{"version": {"1"}}, hx), http.StatusConflict)
			if strings.Contains(h.request("GET", "/post/s/"+p.Slug, nil, hx).Body.String(), "action-publish") {
				t.Fatal("published post still offers Publish")
			}
			h.cookies = make(map[string]*http.Cookie)
			for _, path := range []string{"/post/s/" + p.Slug, "/posts", "/posts/recent", "/rss"} {
				response := h.request("GET", path, nil, false)
				requireStatus(t, response, http.StatusOK)
				if !strings.Contains(response.Body.String(), p.Title) {
					t.Fatalf("published post missing from %s", path)
				}
			}
		})
	}
}

func TestPublishDraftFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		status  int
	}{
		{"missing version", "", http.StatusBadRequest},
		{"invalid version", "bad", http.StatusBadRequest},
		{"stale version", "1", http.StatusConflict},
		{"scheduled", "1", http.StatusConflict},
		{"storage failure", "1", http.StatusInternalServerError},
		{"anonymous", "1", http.StatusForbidden},
		{"other writer", "1", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := usersHarness(t)
			p := Post{Title: "Private writing", Markdown: "Keep private.", Draft: true}
			if test.name == "scheduled" {
				p.Draft, p.Published = false, time.Now().Add(24*time.Hour)
			}
			p, err := h.blog.persistPost(t.Context(), p)
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "storage failure" {
				h.blog.posts = failingRepository{h.blog.posts}
			}
			if test.name == "stale version" {
				p.Title = "Newer writing"
				p, err = h.blog.persistPost(t.Context(), p)
				if err != nil {
					t.Fatal(err)
				}
			}
			if test.name == "other writer" {
				registeredPoster(t, h)
			}
			if test.name == "anonymous" {
				h.cookies = make(map[string]*http.Cookie)
			}
			for _, hx := range []bool{false, true} {
				response := h.request("POST", "/post/publish/"+p.ID, url.Values{"version": {test.version}}, hx)
				requireStatus(t, response, test.status)
				if response.Header().Get("HX-Redirect") != "" || strings.Contains(response.Body.String(), "private storage failure") {
					t.Fatal("failed publication redirected or exposed storage details")
				}
			}
			stored, err := h.blog.posts.GetByID(t.Context(), p.ID, true)
			if err != nil || stored != p {
				t.Fatal("rejected publication changed the post", err)
			}
		})
	}
}

func TestPosterPublishesOwnDraft(t *testing.T) {
	h := usersHarness(t)
	poster := registeredPoster(t, h)
	p, err := h.blog.persistPost(t.Context(), Post{Title: "My draft", Markdown: "My writing.", Draft: true, AuthorID: poster.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.request("GET", "/post/s/"+p.Slug, nil, false).Body.String(), "action-publish") {
		t.Fatal("Poster cannot publish their draft from its preview")
	}
	requireStatus(t, h.request("POST", "/post/publish/"+p.ID, url.Values{"version": {"1"}}, false), http.StatusSeeOther)
	stored, err := h.blog.posts.GetByID(t.Context(), p.ID, false)
	if err != nil || stored.AuthorID != poster.ID {
		t.Fatal("Poster publication failed or changed ownership", err)
	}
}
