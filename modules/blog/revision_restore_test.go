package blog

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRestoreRevisionHTTP exercises direct restoration without trusting submitted writing or settings.
func TestRestoreRevisionHTTP(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		for _, hx := range []bool{false, true} {
			t.Run(kind+"/htmx="+strconv.FormatBool(hx), func(t *testing.T) {
				h := newHarness(t)
				id, versions := revisionDeleteFixture(t, h, kind)
				store := h.blog.posts.(*sqliteRepository)
				base := "/history/" + kind + "/" + id
				path := base + "/restore?revision=" + strconv.FormatInt(versions[2].ID, 10)
				for _, signedIn := range []bool{false, true} {
					if signedIn {
						h.register(t)
						h.login(t)
					}
					requireStatus(t, h.request("POST", path, url.Values{"version": {"4"}}, hx), http.StatusForbidden)
				}
				h.verify(t)
				var beforePost Post
				var beforePage Page
				var token int64
				if kind == "post" {
					p, err := store.GetByID(t.Context(), id, true)
					if err != nil {
						t.Fatal(err)
					}
					p.Draft, p.Slug = hx, "current-url"
					beforePost, err = savePostFixture(store, t.Context(), p)
					if err != nil {
						t.Fatal(err)
					}
					token = beforePost.Version
					// Warm all reader caches before changing the title.
					for _, readerPath := range []string{"/rss", "/posts/recent", "/posts"} {
						h.request("GET", readerPath, nil, false)
					}
				} else {
					p, err := store.GetPageByID(t.Context(), id, true)
					if err != nil {
						t.Fatal(err)
					}
					p.Published, p.Slug, p.ParentID = !hx, "current-url", h.projectsID
					p.ChildrenDisplay, p.DisplayOrder = "blocks", new(7)
					beforePage, err = savePageFixture(store, t.Context(), p)
					if err != nil {
						t.Fatal(err)
					}
					token = beforePage.Version
				}
				beforeRestore, err := store.ListRevisions(t.Context(), kind, id)
				if err != nil || len(beforeRestore) != len(versions)+1 {
					t.Fatal("URL change must preserve a separate rendering base", beforeRestore, err)
				}
				values := url.Values{"version": {strconv.FormatInt(token, 10)}, "title": {"Injected"}, "slug": {"injected"}, "published": {"on"}}
				requireStatus(t, h.request("GET", path, nil, hx), http.StatusNotFound)
				for _, invalid := range []string{"", "0", "invalid"} {
					requireStatus(t, h.request("POST", base+"/restore?revision="+invalid, values, hx), http.StatusNotFound)
				}
				requireStatus(t, h.request("POST", path, nil, hx), http.StatusBadRequest)
				stale := url.Values{"version": {strconv.FormatInt(token-1, 10)}}
				requireStatus(t, h.request("POST", path, stale, hx), http.StatusConflict)
				requireStatus(t, h.request("POST", "/history/"+kind+"/wrong-owner/restore?revision="+strconv.FormatInt(versions[2].ID, 10), values, hx), http.StatusNotFound)
				response := h.request("POST", path, values, hx)
				status, header := http.StatusSeeOther, "Location"
				if hx {
					status, header = http.StatusOK, "HX-Redirect"
				}
				requireStatus(t, response, status)
				if response.Header().Get(header) != base {
					t.Fatal("restore did not return to History", response.Header())
				}
				after := revisionOwner(t, h, kind, id)
				if kind == "post" {
					expected := beforePost
					expected.Title, expected.Markdown, expected.Version = "Second", "Second body", token+1
					if after != expected {
						t.Fatal("content or settings changed incorrectly", after, expected)
					}
					if !hx {
						for _, readerPath := range []string{"/rss", "/posts/recent", "/posts"} {
							body := h.request("GET", readerPath, nil, false).Body.String()
							if !strings.Contains(body, "Second") {
								t.Fatal("stale reader cache", readerPath)
							}
						}
					}
				} else {
					expected := beforePage
					expected.Title, expected.Markdown, expected.Version = "Second", "Second body", token+1
					expected.Updated = after.(Page).Updated
					if !reflect.DeepEqual(after, expected) {
						t.Fatal("page settings changed", after, expected)
					}
				}
				list, err := store.ListRevisions(t.Context(), kind, id)
				if err != nil || len(list) != len(beforeRestore)+1 || list[0].Title != "Second" || !reflect.DeepEqual(list[1:], beforeRestore) {
					t.Fatal(list, err)
				}
				values.Set("version", strconv.FormatInt(token+1, 10))
				requireStatus(t, h.request("POST", path, values, hx), status)
				unchanged, err := store.ListRevisions(t.Context(), kind, id)
				if err != nil || !reflect.DeepEqual(list, unchanged) || !reflect.DeepEqual(after, revisionOwner(t, h, kind, id)) {
					t.Fatal("identical restore changed content or history", err)
				}
				preview := h.request("GET", base+"?revision="+strconv.FormatInt(versions[2].ID, 10), nil, hx)
				if strings.Contains(preview.Body.String(), "Restore this version</button>") {
					t.Fatal("matching version offers restore")
				}
			})
		}
	}
}

// failedRestorePost rejects persistence after validation to test failure reporting.
type failedRestorePost struct{ repository }

func (failedRestorePost) Save(context.Context, Post, []string) (Post, error) {
	return Post{}, errors.New("private write failure")
}

// failedRestorePage rejects persistence after validation to test failure reporting.
type failedRestorePage struct{ pageRepository }

func (failedRestorePage) SavePage(context.Context, Page, []string) (Page, error) {
	return Page{}, errors.New("private write failure")
}

// TestRestoreRevisionFailures preserves current content when old images are missing or storage fails.
func TestRestoreRevisionFailures(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			store := h.blog.posts.(*sqliteRepository)
			file := mediaUpload(t, h.blog, "restore image")
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, Revision{Title: "Original", Markdown: "![Image](/images/" + file.ID + ")", PreviewImage: file.ID})
			if err != nil {
				t.Fatal(err)
			}
			_, version, err = saveRevisionContent(t.Context(), store, kind, id, version, Revision{Title: "Current", Markdown: "Current body"})
			if err != nil {
				t.Fatal(err)
			}
			list, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil {
				t.Fatal(err)
			}
			path := "/history/" + kind + "/" + id + "/restore?revision=" + strconv.FormatInt(list[1].ID, 10)
			before := revisionOwner(t, h, kind, id)
			if kind == "post" {
				h.blog.posts = failedRestorePost{store}
			} else {
				h.blog.pageStore = failedRestorePage{store}
			}
			values := url.Values{"version": {strconv.FormatInt(version, 10)}}
			failure := h.request("POST", path, values, false)
			requireStatus(t, failure, http.StatusInternalServerError)
			if strings.Contains(failure.Body.String(), "private write failure") || failure.Header().Get("Location") != "" {
				t.Fatal("failed restore leaked or redirected")
			}
			h.blog.posts, h.blog.pageStore = store, store
			if err := h.blog.removeMedia(t.Context(), file.ID, file.Version(), true); err != nil {
				t.Fatal(err)
			}
			missing := h.request("POST", path, values, false)
			requireStatus(t, missing, http.StatusUnprocessableEntity)
			if !strings.Contains(missing.Body.String(), "Edit this version to replace or remove") || !strings.Contains(missing.Body.String(), `class="revision-list"`) {
				t.Fatal("missing-image error lost history context")
			}
			after, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil || !reflect.DeepEqual(after, list) || !reflect.DeepEqual(before, revisionOwner(t, h, kind, id)) {
				t.Fatal("failed restore changed content", err)
			}
		})
	}
}

// TestRestoreScheduledRevision retains a pending publication date and keeps the post private.
func TestRestoreScheduledRevision(t *testing.T) {
	h := newHarness(t)
	store := h.blog.posts.(*sqliteRepository)
	p, err := savePostFixture(store, t.Context(), Post{Slug: "scheduled", Title: "Original", Markdown: "Original body", Published: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.ListRevisions(t.Context(), "post", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Title = "Current"
	p, err = savePostFixture(store, t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := store.GetRevision(t.Context(), "post", p.ID, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.blog.restoreRevisionContent(t.Context(), "post", p.ID, p.Version, selected); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetByID(t.Context(), p.ID, true)
	if err != nil || after.Published != p.Published || after.Draft != p.Draft {
		t.Fatal("restore changed schedule", after, err)
	}
	requireStatus(t, h.request("GET", "/post/s/"+p.Slug, nil, false), http.StatusNotFound)
}
