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

func TestRevisionEquivalentTags(t *testing.T) {
	for _, tc := range []struct {
		name, tags, other string
		equal             bool
	}{
		{"order", "ux-testing, tiny-icon", "tiny-icon, ux-testing", true},
		{"case and spacing", "Go,  WEB, go", "go, web", true},
		{"different tags", "go", "web", false},
		{"invalid tags", "\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := Revision{Tags: tc.tags}, Revision{Tags: tc.other}
			if a.sameContent(b) != tc.equal || b.sameContent(a) != tc.equal {
				t.Fatal("incorrect tag comparison")
			}
			b.Markdown = "Changed writing"
			if a.sameContent(b) {
				t.Fatal("equivalent tags hid a writing change")
			}
		})
	}
}

func TestRestoreSavedRelatedReferences(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	ctx := t.Context()
	target, err := h.blog.persistPost(ctx, Post{Title: "Related post", Markdown: "A related article."})
	if err != nil {
		t.Fatal(err)
	}
	targetPage, err := savePageFixture(h.blog.pageStore, ctx, Page{Title: "Related page", Slug: "related-page-target", Published: true})
	if err != nil {
		t.Fatal(err)
	}
	links := []string{relatedPagePrefix + targetPage.ID, target.ID}
	fields := pageFields("related-history-review", true)
	fields.Set("componentOrder", `["related-posts"]`)
	savedRelated := componentJSON(postRelated{Links: links})
	fields.Set("related", savedRelated)
	requireStatus(t, h.request(http.MethodPost, "/pages", fields, false), http.StatusSeeOther)
	page, err := h.blog.pageStore.GetPageBySlug(ctx, fields.Get("slug"), true)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := h.blog.history.ListRevisions(ctx, "page", page.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("expected one page revision, got %d, %v", len(versions), err)
	}
	editor := "/pages/" + page.ID
	query := "?revision=" + strconv.FormatInt(versions[0].ID, 10)
	plain := pageFields(page.Slug, true)
	requireStatus(t, h.submitEditor(t, editor, plain, false), http.StatusSeeOther)
	if err := h.blog.posts.Delete(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.blog.pageStore.DeletePage(ctx, targetPage.ID); err != nil {
		t.Fatal(err)
	}
	setVersion := func() {
		t.Helper()
		current, err := h.blog.pageStore.GetPageByID(ctx, page.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		fields.Set("version", strconv.FormatInt(current.Version, 10))
	}
	setVersion()
	requireStatus(t, h.request(http.MethodPost, editor, fields, true), http.StatusUnprocessableEntity)
	fields.Set("related", componentJSON(postRelated{Links: append(links, strings.Repeat("A", 26))}))
	requireStatus(t, h.request(http.MethodPost, editor+query, fields, true), http.StatusUnprocessableEntity)
	fields.Set("related", savedRelated)
	restore := "/history/page/" + page.ID + "/restore" + query
	requireStatus(t, h.request(http.MethodPost, restore, url.Values{"version": {fields.Get("version")}}, false), http.StatusSeeOther)
	requireStatus(t, h.submitEditor(t, editor, plain, false), http.StatusSeeOther)
	setVersion()
	requireStatus(t, h.request(http.MethodPost, editor+query, fields, true), http.StatusOK)
	current, err := h.blog.pageStore.GetPageByID(ctx, page.ID, true)
	if ids := relatedReferences(current.Markdown); err != nil || !reflect.DeepEqual(ids, links) {
		t.Fatalf("historical reference was not retained: %v, %v", ids, err)
	}
	reader := h.request(http.MethodGet, page.URL(), nil, false)
	requireStatus(t, reader, http.StatusOK)
	if strings.Contains(reader.Body.String(), `class="related-posts"`) {
		t.Fatal("deleted targets must remain hidden after restore")
	}

	// Another editor can delete the source revision while its editor is open.
	if err := h.blog.history.DeleteRevision(ctx, "page", page.ID, versions[0].ID); err != nil {
		t.Fatal(err)
	}
	available, err := h.blog.persistPost(ctx, Post{Title: "Available related post", Markdown: "A link that needs no revision."})
	if err != nil {
		t.Fatal(err)
	}
	fields.Set("related", componentJSON(postRelated{Posts: []string{available.ID}}))
	fields.Set("content", "Writing saved after the original revision was deleted.")
	setVersion()
	requireStatus(t, h.request(http.MethodPost, editor+query, fields, false), http.StatusSeeOther)
	fields.Set("related", savedRelated)
	fields.Set("content", "Keep this edit while I correct the related post.")
	setVersion()
	rejected := h.request(http.MethodPost, editor+query, fields, false)
	requireStatus(t, rejected, http.StatusUnprocessableEntity)
	if body := rejected.Body.String(); !strings.Contains(body, `data-error-fields="related"`) || !strings.Contains(body, fields.Get("content")) {
		t.Fatal("an unverifiable reference must leave the submitted content in the editor")
	}
}

// Imported tag ordering must not offer a restore that normalization would undo.
func TestRestoreRevisionEquivalentTags(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			store := h.blog.posts.(*sqliteRepository)
			content := Revision{Title: "Imported writing", Markdown: "Body", Tags: "ux-testing, tiny-icon"}
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, content)
			if err != nil {
				t.Fatal(err)
			}
			content.Tags = "tiny-icon, ux-testing"
			_, version, err = saveRevisionContent(t.Context(), store, kind, id, version, content)
			if err != nil {
				t.Fatal(err)
			}
			versions, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil || len(versions) != 2 {
				t.Fatal(versions, err)
			}
			base := "/history/" + kind + "/" + id
			query := "?revision=" + strconv.FormatInt(versions[1].ID, 10)
			before := revisionOwner(t, h, kind, id)
			for _, hx := range []bool{false, true} {
				preview := h.request("GET", base+query, nil, hx)
				requireStatus(t, preview, http.StatusOK)
				if strings.Contains(preview.Body.String(), "Restore this version</button>") || !strings.Contains(preview.Body.String(), "This version matches the current content.") {
					t.Fatal("equivalent tags offered a misleading restore")
				}
				response := h.request("POST", base+"/restore"+query, url.Values{"version": {strconv.FormatInt(version, 10)}}, hx)
				status := http.StatusSeeOther
				if hx {
					status = http.StatusOK
				}
				requireStatus(t, response, status)
				after, err := store.ListRevisions(t.Context(), kind, id)
				if err != nil || !reflect.DeepEqual(versions, after) || !reflect.DeepEqual(before, revisionOwner(t, h, kind, id)) {
					t.Fatal("equivalent restore changed content or history", err)
				}
			}
		})
	}
}

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
	if err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), "post", p.ID, p.Version, selected); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetByID(t.Context(), p.ID, true)
	if err != nil || after.Published != p.Published || after.Draft != p.Draft {
		t.Fatal("restore changed schedule", after, err)
	}
	requireStatus(t, h.request("GET", "/post/s/"+p.Slug, nil, false), http.StatusNotFound)
}
