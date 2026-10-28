package blog

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestRevisionPreview keeps targeted selection separate from navigation and history restoration.
func TestRevisionPreview(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			id, versions := revisionDeleteFixture(t, h, kind)
			h.register(t)
			h.login(t)
			h.verify(t)
			path := "/history/" + kind + "/" + id + "?revision=" + strconv.FormatInt(versions[1].ID, 10)
			request := func(path string, hx, restore bool) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "https://blog.example"+path, nil)
				r.Header.Set("HX-Target", "revision-preview")
				r.Header.Set("HX-Request", strconv.FormatBool(hx))
				r.Header.Set("HX-History-Restore-Request", strconv.FormatBool(restore))
				for _, cookie := range h.cookies {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(w, r)
				return w
			}
			for _, tc := range []struct{ hx, restore, fragment bool }{{true, false, true}, {false, false, false}, {true, true, false}} {
				w := request(path, tc.hx, tc.restore)
				requireStatus(t, w, http.StatusOK)
				body := w.Body.String()
				if !strings.Contains(body, "<h2>Third</h2>") || strings.Contains(body, `class="revision-list"`) == tc.fragment || strings.Contains(body, "<!DOCTYPE html>") == tc.fragment {
					t.Fatal("incorrect preview/full-page rendering", body)
				}
				wantURL := ""
				if tc.fragment {
					wantURL = path
				}
				if w.Header().Get("HX-Replace-Url") != wantURL || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Vary"), "HX-Target") {
					t.Fatal("incorrect preview headers", w.Header())
				}
			}
			missing := request(strings.Split(path, "?")[0]+"?revision=999999", true, false)
			requireStatus(t, missing, http.StatusNotFound)
			if missing.Header().Get("HX-Replace-Url") != "" {
				t.Fatal("missing revision changed the history URL")
			}
			h.blog.history = failedHistory{revisionRepository: h.blog.history}
			requireStatus(t, request(path, true, false), http.StatusInternalServerError)
			h.cookies = nil
			requireStatus(t, request(path, true, false), http.StatusForbidden)
		})
	}
}

// revisionForm reads the saved fields that History loads into the real editor.
// Settings are asserted separately so restoration cannot quietly reset them.
func revisionForm(t *testing.T, body string) url.Values {
	t.Helper()
	values := url.Values{}
	for _, name := range []string{"title", "slug", "previewImage", "version"} {
		match := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(body)
		if len(match) != 2 {
			t.Fatal("missing input", name)
		}
		values.Set(name, html.UnescapeString(match[1]))
	}
	for _, name := range []string{"summary", "content"} {
		match := regexp.MustCompile(`(?s)<textarea[^>]*name="` + name + `"[^>]*>(.*?)</textarea>`).FindStringSubmatch(body)
		if len(match) != 2 {
			t.Fatal("missing textarea", name)
		}
		values.Set(name, html.UnescapeString(match[1]))
	}
	return values
}

// TestRevisionHTTP checks page-based browsing and explicit restoration for both
// content types. Loading writing never changes server data or current settings.
func TestRevisionHTTP(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			store := h.blog.posts.(*sqliteRepository)
			imageID, err := h.blog.images.Put(t.Context(), "image/png", testPNG(t))
			if err != nil {
				t.Fatal(err)
			}
			original := Revision{Title: "Earlier <title>", Summary: "Earlier & summary", Markdown: "# Earlier body\n\n<script>alert(1)</script>\n\n[Sibling](sibling)\n\n![Relative image](relative.png)", PreviewImage: imageID}
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, original)
			if err != nil {
				t.Fatal(err)
			}
			list, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil || len(list) != 1 {
				t.Fatal(list, err)
			}
			first := strconv.FormatInt(list[0].ID, 10)
			current := Revision{Title: "Current title", Summary: "Current summary", Markdown: "Current body"}
			_, version, err = saveRevisionContent(t.Context(), store, kind, id, version, current)
			if err != nil {
				t.Fatal(err)
			}
			if public, err := store.ImageIsPublic(t.Context(), "/images/"+imageID); err != nil || public {
				t.Fatal("historical reference made an image public", public, err)
			}
			prefix := "/post"
			var post Post
			var page Page
			if kind == "post" {
				post, err = store.GetByID(t.Context(), id, true)
				if err != nil {
					t.Fatal(err)
				}
				post.Slug, post.Draft = "current-url", true
				post, err = store.Save(t.Context(), post)
				version = post.Version
			} else {
				prefix = "/pages"
				page, err = store.GetPageByID(t.Context(), id, true)
				if err != nil {
					t.Fatal(err)
				}
				page.Slug, page.Published, page.ParentID, page.ChildrenDisplay, page.DisplayOrder = "current-url", false, h.projectsID, "blocks", new(7)
				page, err = store.SavePage(t.Context(), page)
				version = page.Version
			}
			if err != nil {
				t.Fatal(err)
			}
			historyURL, editorURL := "/history/"+kind+"/"+id, prefix+"/update/"+id
			for _, signedIn := range []bool{false, true} {
				if signedIn {
					h.register(t)
					h.login(t)
				}
				for _, path := range []string{historyURL, historyURL + "?revision=" + first, editorURL + "?revision=" + first} {
					for _, hx := range []bool{false, true} {
						response := h.request("GET", path, nil, hx)
						requireStatus(t, response, http.StatusForbidden)
						if response.Header().Get("Cache-Control") != "no-store" {
							t.Fatal("history authorization failure can be cached")
						}
					}
				}
			}
			h.verify(t)
			for _, hx := range []bool{false, true} {
				response := h.request("GET", historyURL, nil, hx)
				requireStatus(t, response, http.StatusOK)
				if response.Header().Get("Cache-Control") != "no-store" || len(sharingTags(t, response.Body.String())) != 0 {
					t.Fatal("history must stay private")
				}
				body := response.Body.String()
				if strings.Contains(body, "<dialog") || strings.Count(body, `<span class="revision-latest">Latest saved</span>`) != 1 || !strings.Contains(body, `">Edit</a>`) || strings.Contains(body, "Edit this version") {
					t.Fatal("current history must be a read-only page", body)
				}
				response = h.request("GET", historyURL+"?revision="+first, nil, hx)
				requireStatus(t, response, http.StatusOK)
				body = response.Body.String()
				if strings.Count(body, `<span class="revision-latest">Latest saved</span>`) != 1 {
					t.Fatal("earlier version must not be labeled current")
				}
				for _, want := range []string{"Earlier &lt;title&gt;", "<h1>Earlier body</h1>", "&lt;script&gt;alert(1)&lt;/script&gt;", "Markdown source", `href="` + editorURL + "?revision=" + first + `"`, "Edit this version", "/images/" + imageID, "Back to " + kind} {
					if !strings.Contains(body, want) {
						t.Fatal("history preview missing", want)
					}
				}
				if strings.Contains(body, "<script>alert(1)</script>") {
					t.Fatal("history rendered unsafe HTML")
				}
				base := "/post/s/"
				if kind == "page" {
					base = "/page/"
				}
				if !strings.Contains(body, `href="`+base+`sibling"`) || !strings.Contains(body, `src="`+base+`relative.png"`) {
					t.Fatal("preview resolved relative URLs against History instead of the content URL")
				}
				if !strings.Contains(body, `href="`+base+`current-url">Back to `+kind+`</a>`) {
					t.Fatal("history must return to the current content URL")
				}
			}
			for _, selection := range []string{"0", "-1", "invalid", "9999999999999999999999", "999999"} {
				for _, path := range []string{historyURL, editorURL} {
					requireStatus(t, h.request("GET", path+"?revision="+selection, nil, false), http.StatusNotFound)
				}
			}
			requireStatus(t, h.request("GET", "/history/unknown/"+id, nil, false), http.StatusNotFound)
			requireStatus(t, h.request("GET", "/history/"+kind+"/missing", nil, false), http.StatusNotFound)
			if kind == "page" {
				requireStatus(t, h.request("GET", "/pages/update/"+h.projectsID+"?revision="+first, nil, false), http.StatusNotFound)
			}

			response := h.request("GET", editorURL+"?revision="+first, nil, false)
			requireStatus(t, response, http.StatusOK)
			body := response.Body.String()
			values := revisionForm(t, body)
			if values.Get("title") != original.Title || values.Get("summary") != original.Summary || values.Get("content") != original.Markdown || values.Get("previewImage") != imageID || values.Get("slug") != "current-url" || values.Get("version") != strconv.FormatInt(version, 10) {
				t.Fatal("loaded editor has wrong content or settings", values)
			}
			if !strings.Contains(body, "Editing a version saved") || !strings.Contains(body, "Nothing changes until you save.") || !strings.Contains(body, "data-current-content=") || strings.Contains(body, `name="published" checked`) || strings.Contains(body, `value="on" selected`) {
				t.Fatal("loaded version changed publication or lacks unsaved-work guidance")
			}
			if kind == "post" {
				stored, err := store.GetByID(t.Context(), id, true)
				if err != nil || stored != post {
					t.Fatal("browsing history changed the post", stored, err)
				}
			} else {
				for _, want := range []string{`value="` + h.projectsID + `" selected`, `value="blocks" selected`, `pattern="[0-9]+" value="7"`} {
					if !strings.Contains(body, want) {
						t.Fatal("loading history lost page placement", want)
					}
				}
				stored, err := store.GetPageByID(t.Context(), id, true)
				if err != nil || !reflect.DeepEqual(stored, page) {
					t.Fatal("browsing history changed the page", stored, err)
				}
				values.Set("parentID", h.projectsID)
				values.Set("displayOrder", "7")
				values.Set("childrenDisplay", "blocks")
			}
			action := prefix + "/" + id + "?revision=" + first
			cancelLink := `href="` + historyURL + `?revision=` + first + `" hx-boost="false">Cancel</a>`
			if !strings.Contains(body, cancelLink) || !strings.Contains(body, `action="`+action+`"`) {
				t.Fatal("historical editor lost its history context")
			}
			for _, status := range []int{http.StatusUnprocessableEntity, http.StatusConflict} {
				if status == http.StatusUnprocessableEntity {
					values.Set("title", "")
				} else {
					values.Set("title", "Unsaved writing")
					values.Set("version", "0")
				}
				response := h.request("POST", action, values, false)
				requireStatus(t, response, status)
				body := response.Body.String()
				if !strings.Contains(body, cancelLink) || !strings.Contains(body, `action="`+action+`"`) || revisionForm(t, body).Get("title") != values.Get("title") {
					t.Fatal("failed save lost history context or replaced submitted writing")
				}
			}
			values.Set("title", original.Title)
			values.Set("version", strconv.FormatInt(version, 10))
			// Only this explicit save appends the restored content. Drafts stay private.
			requireStatus(t, h.request("POST", action, values, false), http.StatusSeeOther)
			list, err = store.ListRevisions(t.Context(), kind, id)
			if err != nil || len(list) != 3 || list[0].Title != original.Title || list[1].Title != current.Title {
				t.Fatal("restoration discarded history", list, err)
			}
			h.cookies = nil
			if kind == "post" {
				requireStatus(t, h.request("GET", "/post/s/current-url", nil, false), http.StatusNotFound)
			} else {
				requireStatus(t, h.request("GET", "/page/current-url", nil, false), http.StatusNotFound)
			}
			requireStatus(t, h.request("GET", "/images/"+imageID, nil, false), http.StatusNotFound)
		})
	}
}

// TestEditorVersionHTTP keeps stale and malformed submissions out of storage.
// Ordinary validation errors must retain the version so corrections can be saved.
func TestEditorVersionHTTP(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			store := h.blog.posts.(*sqliteRepository)
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, Revision{Title: "Original", Markdown: "Body"})
			if err != nil {
				t.Fatal(err)
			}
			prefix := "/post"
			if kind == "page" {
				prefix = "/pages"
			}
			action := prefix + "/" + id
			values := url.Values{"title": {"Unsaved title"}, "slug": {"history"}, "content": {"Unsaved body"}, "version": {strconv.FormatInt(version, 10)}}
			values.Set("title", "")
			response := h.request("POST", action, values, false)
			requireStatus(t, response, http.StatusUnprocessableEntity)
			if revisionForm(t, response.Body.String()).Get("version") != values.Get("version") {
				t.Fatal("validation discarded edit version")
			}
			if !strings.Contains(response.Body.String(), "data-unsaved") {
				t.Fatal("validation response treats unsaved input as saved content")
			}
			values.Set("title", "Unsaved title")
			requireStatus(t, h.request("POST", action, values, false), http.StatusSeeOther)
			for _, token := range []string{"1", "", "0", "-1", "invalid", "9999999999999999999999"} {
				values.Set("version", token)
				for _, hx := range []bool{false, true} {
					response := h.request("POST", action, values, hx)
					requireStatus(t, response, http.StatusConflict)
					body := response.Body.String()
					if !strings.Contains(body, editConflictMessage) || response.Header().Get("HX-Trigger") != "" || response.Header().Get("HX-Redirect") != "" || response.Header().Get("Location") != "" {
						t.Fatal("conflict lacked feedback or acknowledged save")
					}
					if hx && strings.Contains(body, "<form") {
						t.Fatal("conflict replaced the mounted editor")
					}
					if !hx && (revisionForm(t, body).Get("title") != "Unsaved title" || revisionForm(t, body).Get("content") != "Unsaved body") {
						t.Fatal("ordinary conflict discarded input")
					}
					if !hx && !strings.Contains(body, "data-unsaved") {
						t.Fatal("conflict response treats rejected input as saved content")
					}
				}
			}
			if versions, err := store.ListRevisions(t.Context(), kind, id); err != nil || len(versions) != 2 {
				t.Fatal("conflicts wrote revisions", versions, err)
			}
		})
	}
}

type failedHistory struct {
	revisionRepository
	lookup bool
}

// TestHistoryActions keeps history out of editors and offers it only when an
// earlier content snapshot exists, not merely a newer concurrency token.
func TestHistoryActions(t *testing.T) {
	for _, kind := range []string{"post", "page"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			store := h.blog.posts.(*sqliteRepository)
			content := Revision{Title: "History actions", Markdown: "First body"}
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, content)
			if err != nil {
				t.Fatal(err)
			}
			prefix, view, manager := "/post", "/post/s/history", "/post/manage"
			if kind == "page" {
				prefix, view, manager = "/pages", "/page/history", "/pages"
			}
			link := `href="/history/` + kind + `/` + id + `"`
			for _, changed := range []bool{false, true} {
				if changed {
					content.Markdown = "Second body"
				}
				_, version, err = saveRevisionContent(t.Context(), store, kind, id, version, content)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "page" {
					p, err := store.GetPageByID(t.Context(), id, true)
					if err != nil {
						t.Fatal(err)
					}
					p.ParentID = h.projectsID
					p, err = store.SavePage(t.Context(), p)
					if err != nil {
						t.Fatal(err)
					}
					version = p.Version
				}
				for _, hx := range []bool{false, true} {
					for _, path := range []string{view, manager, prefix + "/update/" + id} {
						response := h.request("GET", path, nil, hx)
						requireStatus(t, response, http.StatusOK)
						want := changed && !strings.Contains(path, "/update/")
						if strings.Contains(response.Body.String(), link) != want {
							t.Fatalf("%s HX=%v: history visible=%v, want %v", path, hx, !want, want)
						}
					}
				}
			}
			h.cookies = nil
			response := h.request("GET", view, nil, false)
			requireStatus(t, response, http.StatusOK)
			if strings.Contains(response.Body.String(), link) {
				t.Fatal("anonymous view exposes history action")
			}
		})
	}
}

type failedHistoryAvailability struct{ revisionRepository }

func (failedHistoryAvailability) HasHistory(context.Context, string, string) (bool, error) {
	return false, errors.New("private history availability failure")
}

func (failedHistoryAvailability) HistoryOwners(context.Context, string) (map[string]bool, error) {
	return nil, errors.New("private history availability failure")
}

// TestHistoryAvailabilityFailure reports failed administrator lookups without
// exposing storage errors or making public content depend on history queries.
func TestHistoryAvailabilityFailure(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	_, _, err := saveRevisionContent(t.Context(), h.blog.posts.(*sqliteRepository), "post", "", 0, Revision{Title: "Post", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	h.blog.history = failedHistoryAvailability{h.blog.history}
	for _, path := range []string{"/post/manage", "/pages", "/post/s/history", "/projects", "/"} {
		for _, hx := range []bool{false, true} {
			response := h.request("GET", path, nil, hx)
			requireStatus(t, response, http.StatusInternalServerError)
			if strings.Contains(response.Body.String(), "private history availability failure") {
				t.Fatal("response exposed storage error")
			}
		}
	}
	h.cookies = nil
	for _, path := range []string{"/post/s/history", "/projects", "/"} {
		requireStatus(t, h.request("GET", path, nil, false), http.StatusOK)
	}
}

func (f failedHistory) ListRevisions(ctx context.Context, kind, id string) ([]Revision, error) {
	if f.lookup {
		return f.revisionRepository.ListRevisions(ctx, kind, id)
	}
	return nil, errors.New("private history failure")
}

func (failedHistory) GetRevision(context.Context, string, string, int64) (Revision, error) {
	return Revision{}, errors.New("private history failure")
}

// TestRevisionFailures distinguishes unavailable history from missing content.
func TestRevisionFailures(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	history := h.blog.history
	for _, lookup := range []bool{false, true} {
		h.blog.history = failedHistory{history, lookup}
		for _, hx := range []bool{false, true} {
			for _, path := range []string{"/history/page/" + h.projectsID, "/pages/update/" + h.projectsID + "?revision=1"} {
				response := h.request("GET", path, nil, hx)
				requireStatus(t, response, http.StatusInternalServerError)
				if strings.Contains(response.Body.String(), "private history failure") || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("history failure exposed details or cached a response")
				}
			}
		}
	}
}
