package blog

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func previewFields(h *harness, slug string, published bool) url.Values {
	v := url.Values{"parentID": {h.projectsID}, "slug": {slug}, "title": {"Project " + slug}, "summary": {"A short description"}, "content": {"# Details\n\nProject content"}, "displayOrder": {"10"}}
	if published {
		v.Set("published", "on")
	}
	return v
}

// TestPagePreviewsHTTP exercises draft privacy, edits, publication, and image removal
// through ordinary and HTMX requests using real storage and authentication.
func TestPagePreviewsHTTP(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "HTMX"}[hx], func(t *testing.T) {
			h := newHarness(t)
			for _, path := range []string{"/pages/create", "/pages/update/missing", "/pages/delete/missing"} {
				requireStatus(t, h.request("GET", path, nil, hx), http.StatusForbidden)
			}
			for _, path := range []string{"/pages", "/pages/missing", "/pages/delete/missing"} {
				requireStatus(t, h.request("POST", path, previewFields(h, "blocked", true), hx), http.StatusForbidden)
			}
			h.register(t)
			h.login(t)
			requireStatus(t, h.request("POST", "/pages", previewFields(h, "unverified", true), hx), http.StatusForbidden)
			h.verify(t)
			redirectStatus, header := http.StatusSeeOther, "Location"
			if hx {
				redirectStatus, header = http.StatusOK, "HX-Redirect"
			}
			fields := previewFields(h, "draft", false)
			w := h.request("POST", "/pages", fields, hx)
			requireStatus(t, w, redirectStatus)
			if w.Header().Get(header) != "/page/draft" {
				t.Fatal("wrong project redirect", w.Header())
			}
			p, err := h.blog.pageStore.GetPageBySlug(t.Context(), "draft", true)
			if err != nil {
				t.Fatal(err)
			}
			w = h.request("GET", "/projects", nil, hx)
			requireStatus(t, w, http.StatusOK)
			if !strings.Contains(w.Body.String(), "Draft") || !strings.Contains(w.Body.String(), "/assets/img/project-default.svg") {
				t.Fatal("missing draft card or default image", w.Body)
			}
			cookies := h.cookies
			h.cookies = nil
			requireStatus(t, h.request("GET", "/page/draft", nil, hx), http.StatusNotFound)
			if strings.Contains(h.request("GET", "/projects", nil, hx).Body.String(), "/page/draft") {
				t.Fatal("draft listed publicly")
			}
			h.cookies = cookies

			var imageData bytes.Buffer
			if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 20, 10))); err != nil {
				t.Fatal(err)
			}
			imageID, err := h.blog.images.Put(t.Context(), "image/png", imageData.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			fields.Set("published", "on")
			fields.Set("previewImage", imageID)
			fields.Set("slug", "published")
			requireStatus(t, h.submitEditor(t, "/pages/"+p.ID, fields, hx), redirectStatus)
			h.cookies = nil
			w = h.request("GET", "/projects", nil, hx)
			if !strings.Contains(w.Body.String(), "/images/"+imageID) || !strings.Contains(w.Body.String(), "/page/published") {
				t.Fatal("published card missing", w.Body)
			}
			w = h.request("GET", "/page/published", nil, hx)
			requireStatus(t, w, http.StatusOK)
			// Sharing metadata may use the preview image, but the visible detail must not.
			_, detail, fullPage := strings.Cut(w.Body.String(), "</head>")
			if !fullPage {
				detail = w.Body.String()
			}
			if strings.Contains(detail, imageID) || !strings.Contains(detail, "<h1>Details</h1>") {
				t.Fatal("detail image leakage or incorrect Markdown", w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("project response may retain draft content in caches")
			}
			for _, path := range []string{"/rss", "/posts/recent"} {
				if strings.Contains(h.request("GET", path, nil, true).Body.String(), "Project draft") {
					t.Fatal("projects leaked into post feed", path)
				}
			}
			h.cookies = cookies
			// Replacing a preview changes the card reference without changing its body.
			replacement, err := h.blog.images.Put(t.Context(), "image/png", testPNG(t))
			if err != nil {
				t.Fatal(err)
			}
			fields.Set("previewImage", replacement)
			requireStatus(t, h.submitEditor(t, "/pages/"+p.ID, fields, hx), redirectStatus)
			if saved, err := h.blog.pageStore.GetPageByID(t.Context(), p.ID, true); err != nil || saved.PreviewImage != replacement || saved.Markdown != fields.Get("content") {
				t.Fatal("preview replacement failed", saved, err)
			}
			fields.Set("previewImage", "")
			fields.Del("published")
			requireStatus(t, h.submitEditor(t, "/pages/"+p.ID, fields, hx), redirectStatus)
			updated, err := h.blog.pageStore.GetPageByID(t.Context(), p.ID, true)
			if err != nil || updated.Published || updated.PreviewImage != "" {
				t.Fatal("unpublish or image removal failed", updated, err)
			}
			h.cookies = nil
			requireStatus(t, h.request("GET", "/page/published", nil, hx), http.StatusNotFound)
			h.cookies = cookies
			requireStatus(t, h.request("GET", "/pages/delete/"+p.ID, nil, hx), http.StatusOK)
			requireStatus(t, h.request("POST", "/pages/delete/"+p.ID, nil, hx), redirectStatus)
			requireStatus(t, h.request("POST", "/pages/delete/"+p.ID, nil, hx), http.StatusNotFound)
			requireStatus(t, h.request("POST", "/pages/"+p.ID, fields, hx), http.StatusNotFound)
		})
	}
}

// TestPagePreviewValidation keeps invalid input out of storage and preserves editor errors.
func TestPagePreviewValidation(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, field := range []string{"title", "summary", "content", "slug", "displayOrder", "previewImage", "published"} {
		t.Run(field, func(t *testing.T) {
			for _, hx := range []bool{false, true} {
				fields := previewFields(h, "valid", true)
				bad := map[string]string{"title": "", "summary": strings.Repeat("x", 2001), "content": strings.Repeat("x", maxMarkdownBytes+1), "slug": "../bad", "displayOrder": "-1", "previewImage": "../../private", "published": "maybe"}
				fields.Set(field, bad[field])
				w := h.request("POST", "/pages", fields, hx)
				if w.Code < 400 || w.Code >= 500 || w.Header().Get("HX-Redirect") != "" {
					t.Fatalf("invalid %s: %d %s", field, w.Code, w.Body)
				}
				if !hx && !strings.Contains(w.Body.String(), `id="page-form"`) {
					t.Fatal("ordinary error lost editor")
				}
			}
		})
	}
	if projects, err := h.blog.pageStore.ListPages(t.Context(), true); err != nil || len(projects) != 2 {
		t.Fatal("invalid input persisted", projects, err)
	}
	fields := previewFields(h, "duplicate", true)
	requireStatus(t, h.request("POST", "/pages", fields, false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/pages", fields, true), http.StatusConflict)
	fields.Set("slug", "escaped")
	fields.Set("title", `<script>alert(1)</script>`)
	fields.Set("content", `<script>alert(1)</script>`)
	requireStatus(t, h.request("POST", "/pages", fields, false), http.StatusSeeOther)
	if strings.Contains(h.request("GET", "/page/escaped", nil, false).Body.String(), `<script>alert(1)</script>`) {
		t.Fatal("project content was not escaped")
	}
	h.blog.images = failingImageStore{}
	fields = previewFields(h, "image-failure", true)
	fields.Set("previewImage", "image")
	w := h.request("POST", "/pages", fields, true)
	requireStatus(t, w, http.StatusInternalServerError)
	if w.Header().Get("HX-Redirect") != "" {
		t.Fatal("failed image lookup produced success")
	}
}

// TestPagePreviewStorage checks preview ordering, unique slugs, persistence, and cancellation.
func TestPagePreviewStorage(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "blog.db?_journal=WAL"
	directory := t.TempDir()
	store, closeStore, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore() })
	firstOrder, lastOrder := 0, 20
	for _, p := range []Page{
		{Slug: "last", Title: "A", DisplayOrder: &lastOrder, Published: true, Markdown: "body"},
		{Slug: "first", Title: "B", DisplayOrder: &firstOrder, Published: true},
		{Slug: "draft", Title: "C", DisplayOrder: &firstOrder},
	} {
		if _, err := store.SavePage(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := store.ListPages(t.Context(), true)
	if err != nil || len(projects) != 3 || projects[0].Slug != "draft" || projects[1].Slug != "first" || projects[2].Slug != "last" || projects[2].Markdown != "" {
		t.Fatal("incorrect project listing", projects, err)
	}
	if _, err := store.GetPageBySlug(t.Context(), "draft", false); !errors.Is(err, errNotFound) {
		t.Fatal("draft visible", err)
	}
	if _, err := store.SavePage(t.Context(), Page{ID: "missing"}); !errors.Is(err, errNotFound) {
		t.Fatal("update inserted missing project", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.SavePage(t.Context(), Page{Slug: "concurrent"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, errPageSlugTaken) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d duplicate slug winners", winners)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ListPages(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored cancellation", err)
	}
	if err := closeStore(); err != nil {
		t.Fatal(err)
	}
	store, closeAgain, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAgain()
	if p, err := store.GetPageBySlug(t.Context(), "last", false); err != nil || p.Markdown != "body" {
		t.Fatal("project not persisted", p, err)
	}
}

// TestPagePreviewSortOrder checks explicit zero, matching orders, unordered projects,
// stable timestamp ties, and the update time assigned when a project is saved.
func TestPagePreviewSortOrder(t *testing.T) {
	h := newHarness(t)
	store := h.blog.pageStore.(*sqliteRepository)
	zero, ten := 0, 10
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		slug  string
		order *int
		age   time.Duration
	}{
		{"unordered-old", nil, time.Hour},
		{"ordered-old", &ten, time.Hour},
		{"unordered-new", nil, 2 * time.Hour},
		{"ordered-new", &ten, 2 * time.Hour},
		{"zero", &zero, 0},
	} {
		p, err := store.SavePage(t.Context(), Page{Slug: item.slug, DisplayOrder: item.order, Published: true})
		if err != nil {
			t.Fatal(err)
		}
		if p.Updated.IsZero() {
			t.Fatal("creation did not record update time")
		}
		if _, err := store.db.ExecContext(t.Context(), "UPDATE blog_pages SET updated=? WHERE id=?", base.Add(item.age).UnixNano(), p.ID); err != nil {
			t.Fatal(err)
		}
	}
	assertOrder := func(want string) {
		t.Helper()
		projects, err := store.ListPages(t.Context(), true)
		if err != nil {
			t.Fatal(err)
		}
		var slugs []string
		for _, p := range projects {
			if p.Slug == "about" || p.Slug == "projects" {
				continue
			}
			slugs = append(slugs, p.Slug)
		}
		if got := strings.Join(slugs, ","); got != want {
			t.Fatalf("order=%s, want %s", got, want)
		}
	}
	assertOrder("zero,ordered-new,ordered-old,unordered-new,unordered-old")
	p, err := store.GetPageBySlug(t.Context(), "ordered-old", true)
	if err != nil {
		t.Fatal(err)
	}
	oldUpdated := p.Updated
	p, err = store.SavePage(t.Context(), p)
	if err != nil || !p.Updated.After(oldUpdated) {
		t.Fatal("save did not update timestamp", p, err)
	}
	assertOrder("zero,ordered-old,ordered-new,unordered-new,unordered-old")
	p.DisplayOrder = nil
	if _, err := store.SavePage(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	assertOrder("zero,ordered-new,ordered-old,unordered-new,unordered-old")
	// Equal timestamps use ID, not title or unstable row ordering.
	if _, err := store.db.ExecContext(t.Context(), "UPDATE blog_pages SET updated=? WHERE display_order IS NULL", base.UnixNano()); err != nil {
		t.Fatal(err)
	}
	pages, err := store.ListPages(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	previousID := ""
	for _, page := range pages {
		if page.DisplayOrder == nil {
			if page.ID < previousID {
				t.Fatal("equal timestamps did not sort by ID")
			}
			previousID = page.ID
		}
	}
}

// TestPagePreviewOptionalOrder checks empty form values, explicit zero, clearing an
// existing order, invalid numbers, and rendering the field without a nil placeholder.
func TestPagePreviewOptionalOrder(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	fields := previewFields(h, "optional", true)
	fields.Del("displayOrder")
	requireStatus(t, h.request("POST", "/pages", fields, false), http.StatusSeeOther)
	p, err := h.blog.pageStore.GetPageBySlug(t.Context(), "optional", true)
	if err != nil || p.DisplayOrder != nil {
		t.Fatal("missing order must be nil", p, err)
	}
	for _, value := range []string{"0", "1000000", ""} {
		fields.Set("displayOrder", value)
		requireStatus(t, h.submitEditor(t, "/pages/"+p.ID, fields, false), http.StatusSeeOther)
		w := h.request("GET", "/pages/update/"+p.ID, nil, false)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), `name="displayOrder" inputmode="numeric" pattern="[0-9]+" value="`+value+`"`) {
			t.Fatal("order field did not round-trip", w.Body)
		}
	}
	for _, invalid := range []string{"-1", "1.5", "1000001", "word", "9999999999999999999999999"} {
		fields.Set("displayOrder", invalid)
		requireStatus(t, h.submitEditor(t, "/pages/"+p.ID, fields, true), http.StatusUnprocessableEntity)
	}
	p, err = h.blog.pageStore.GetPageByID(t.Context(), p.ID, true)
	if err != nil || p.DisplayOrder != nil {
		t.Fatal("cleared order was not persisted", p, err)
	}
}

// TestPagePreviewStorageFailures ensures failures do not produce successful redirects.
func TestPagePreviewStorageFailures(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	h.blog.pageStore = unavailablePages{}
	for _, route := range []struct{ method, path string }{
		{"POST", "/pages"}, {"POST", "/pages/delete/missing"}, {"GET", "/projects"},
		{"GET", "/page/missing"}, {"GET", "/pages/update/missing"}, {"GET", "/pages/delete/missing"},
	} {
		w := h.request(route.method, route.path, previewFields(h, "failed", true), true)
		requireStatus(t, w, http.StatusInternalServerError)
		if strings.Contains(w.Body.String(), "private storage failure") || w.Header().Get("HX-Redirect") != "" {
			t.Fatal("storage failure leaked or redirected", w.Body)
		}
	}
}

// TestPagePreviewOtherAccount verifies that a signed-in account is not sufficient for
// draft access or publishing, even if that account has verified its email.
func TestPagePreviewOtherAccount(t *testing.T) {
	h := newHarness(t)
	p, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "private", Title: "Private draft"})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/page/private", http.StatusNotFound},
		{"GET", "/pages/update/" + p.ID, http.StatusForbidden},
		{"POST", "/pages/" + p.ID, http.StatusForbidden},
		{"POST", "/pages/delete/" + p.ID, http.StatusForbidden},
	} {
		r := user.AddUserToRequestContext(httptest.NewRequest(route.method, route.path, nil), &user.User{Email: "someone@example.com", Verified: true})
		w := httptest.NewRecorder()
		// Supply identity directly to isolate module authorization from the login allowlist.
		h.app.Web.ServeHTTP(w, r)
		requireStatus(t, w, route.status)
	}
}
