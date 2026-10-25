package blog

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// pageFields supplies valid form values, leaving publication unchecked for drafts.
func pageFields(slug string, published bool) url.Values {
	values := url.Values{"slug": {slug}, "title": {"Page " + slug}, "content": {"A **useful** page."}}
	if published {
		values.Set("published", "on")
	}
	return values
}

// TestPagesHTTP checks protected management, draft privacy, About routing, and deletion.
func TestPagesHTTP(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "HTMX"}[hx], func(t *testing.T) {
			h := newHarness(t)
			assertDenied := func() {
				t.Helper()
				for _, path := range []string{"/pages", "/pages/create", "/pages/update/missing", "/pages/delete/missing"} {
					requireStatus(t, h.request("GET", path, nil, hx), http.StatusForbidden)
				}
				for _, path := range []string{"/pages", "/pages/missing", "/pages/delete/missing"} {
					requireStatus(t, h.request("POST", path, pageFields("blocked", true), hx), http.StatusForbidden)
				}
			}
			assertDenied()
			h.register(t)
			h.login(t)
			assertDenied()
			h.verify(t)
			status, header := http.StatusSeeOther, "Location"
			if hx {
				status, header = http.StatusOK, "HX-Redirect"
			}
			fields := pageFields("draft", false)
			w := h.request("POST", "/pages", fields, hx)
			requireStatus(t, w, status)
			if w.Header().Get(header) != "/page/draft" {
				t.Fatal("wrong page redirect", w.Header())
			}
			p, err := h.blog.pageStore.GetPageBySlug(t.Context(), "draft", true)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/pages", "/pages/create", "/pages/update/" + p.ID, "/page/draft"} {
				w = h.request("GET", path, nil, hx)
				requireStatus(t, w, http.StatusOK)
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("page management or draft content can be cached", path)
				}
			}
			cookies := h.cookies
			h.cookies = nil
			requireStatus(t, h.request("GET", "/page/draft", nil, hx), http.StatusNotFound)
			h.cookies = cookies

			fields.Set("slug", "published")
			fields.Set("published", "on")
			fields.Set("content", "A **useful** page.\n\n<script>alert(1)</script>")
			requireStatus(t, h.request("POST", "/pages/"+p.ID, fields, hx), status)
			h.cookies = nil
			requireStatus(t, h.request("GET", "/page/draft", nil, hx), http.StatusNotFound)
			w = h.request("GET", "/page/published", nil, hx)
			requireStatus(t, w, http.StatusOK)
			if !strings.Contains(w.Body.String(), "<strong>useful</strong>") || strings.Contains(w.Body.String(), "<script>alert(1)</script>") || strings.Contains(w.Body.String(), "Edit page") {
				t.Fatal("unsafe or incorrect public page", w.Body)
			}
			for _, path := range []string{"/rss", "/posts/recent", "/posts"} {
				if strings.Contains(h.request("GET", path, nil, hx).Body.String(), "Page draft") {
					t.Fatal("page leaked into post collections", path)
				}
			}
			if strings.Contains(h.request("GET", "/projects", nil, false).Body.String(), "/page/published") {
				t.Fatal("page automatically appeared in navigation")
			}
			h.cookies = cookies

			// About uses exactly the same storage, publication rules, and editor.
			about, err := h.blog.pageStore.GetPageBySlug(t.Context(), "about", true)
			if err != nil {
				t.Fatal(err)
			}
			fields = pageFields("about", false)
			fields.Set("title", "About this site")
			fields.Set("content", "")
			w = h.request("POST", "/pages/"+about.ID, fields, hx)
			requireStatus(t, w, status)
			if w.Header().Get(header) != "/about" {
				t.Fatal("About lost its short URL")
			}
			requireStatus(t, h.request("GET", "/about", nil, hx), http.StatusOK)
			h.cookies = nil
			for _, path := range []string{"/about", "/page/about"} {
				requireStatus(t, h.request("GET", path, nil, hx), http.StatusNotFound)
			}
			h.cookies = cookies
			fields.Set("published", "on")
			requireStatus(t, h.request("POST", "/pages/"+about.ID, fields, hx), status)
			h.cookies = nil
			for _, path := range []string{"/about", "/page/about"} {
				w = h.request("GET", path, nil, hx)
				requireStatus(t, w, http.StatusOK)
				if !strings.Contains(w.Body.String(), "<h1>About this site</h1>") {
					t.Fatal("page title is not data driven")
				}
			}
			h.cookies = cookies
			requireStatus(t, h.request("GET", "/pages/delete/"+p.ID, nil, hx), http.StatusOK)
			w = h.request("POST", "/pages/delete/"+p.ID, nil, hx)
			requireStatus(t, w, status)
			if w.Header().Get(header) != "/pages" {
				t.Fatal("delete did not return to page management")
			}
			for _, path := range []string{"/page/published", "/pages/update/" + p.ID, "/pages/delete/" + p.ID} {
				requireStatus(t, h.request("GET", path, nil, hx), http.StatusNotFound)
			}
			requireStatus(t, h.request("POST", "/pages/"+p.ID, fields, hx), http.StatusNotFound)
			requireStatus(t, h.request("POST", "/pages/delete/"+p.ID, nil, hx), http.StatusNotFound)
		})
	}
}

// TestPageValidation checks invalid fields, escaped content, and conflicting slugs.
func TestPageValidation(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, tt := range []struct{ field, value string }{
		{"title", ""}, {"title", strings.Repeat("a", 241)}, {"title", string([]byte{0xff})},
		{"slug", ""}, {"slug", "../bad"}, {"slug", "About"}, {"slug", strings.Repeat("a", 121)},
		{"content", string([]byte{0xff})}, {"content", strings.Repeat("x", maxMarkdownBytes+1)}, {"published", "maybe"},
	} {
		for _, hx := range []bool{false, true} {
			fields := pageFields("valid", true)
			fields.Set(tt.field, tt.value)
			w := h.request("POST", "/pages", fields, hx)
			if w.Code < 400 || w.Code >= 500 || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
				t.Fatal("invalid page accepted", tt.field, w.Code)
			}
			if !hx && !strings.Contains(w.Body.String(), `id="page-form"`) {
				t.Fatal("ordinary error lost editor")
			}
			if hx && strings.Contains(w.Body.String(), `id="page-form"`) {
				t.Fatal("HTMX error replaced mounted editor")
			}
		}
	}
	if pages, err := h.blog.pageStore.ListPages(t.Context()); err != nil || len(pages) != 1 {
		t.Fatal("invalid input persisted", pages, err)
	}
	fields := pageFields("about", true)
	requireStatus(t, h.request("POST", "/pages", fields, true), http.StatusConflict)
	fields.Set("slug", "escaped")
	fields.Set("title", `<script>alert(1)</script>`)
	fields.Set("content", `<script>alert(1)</script>`)
	requireStatus(t, h.request("POST", "/pages", fields, false), http.StatusSeeOther)
	if strings.Contains(h.request("GET", "/page/escaped", nil, false).Body.String(), `<script>alert(1)</script>`) {
		t.Fatal("page HTML was not escaped")
	}
	p, err := h.blog.pageStore.GetPageBySlug(t.Context(), "escaped", true)
	if err != nil {
		t.Fatal(err)
	}
	fields.Set("slug", "about")
	requireStatus(t, h.request("POST", "/pages/"+p.ID, fields, false), http.StatusConflict)
	if saved, err := h.blog.pageStore.GetPageByID(t.Context(), p.ID); err != nil || saved.Slug != "escaped" {
		t.Fatal("conflict changed page", saved, err)
	}
}

// TestPageOversizedDraft preserves complete submissions without saving invalid content.
func TestPageOversizedDraft(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	original, err := h.blog.pageStore.GetPageBySlug(t.Context(), "about", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/pages", "/pages/" + original.ID} {
		for _, published := range []bool{false, true} {
			for _, hx := range []bool{false, true} {
				fields := pageFields("unsaved-page", published)
				fields.Set("title", `Unsaved <title> & "notes"`)
				fields.Set("content", "</textarea><script>alert(1)</script>"+strings.Repeat("x", maxMarkdownBytes))
				w := h.request("POST", path, fields, hx)
				requireStatus(t, w, http.StatusUnprocessableEntity)
				body := w.Body.String()
				if w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
					t.Fatal("invalid draft redirected")
				}
				if hx {
					if strings.Contains(body, `id="page-form"`) || !strings.Contains(body, "256 KiB") {
						t.Fatal("HTMX error must preserve the mounted editor and explain the limit")
					}
					continue
				}
				for _, field := range []string{"title", "slug", "content"} {
					if !strings.Contains(body, html.EscapeString(fields.Get(field))) {
						t.Fatalf("%s lost submitted %s", path, field)
					}
				}
				if strings.Contains(body, `<script>alert(1)</script>`) {
					t.Fatal("draft was not escaped")
				}
				if strings.Contains(body, `name="published" checked`) != published {
					t.Fatal("publication setting was lost")
				}
				if !strings.Contains(body, `action="`+path+`"`) {
					t.Fatal("editor lost its save destination")
				}
			}
		}
	}
	if saved, err := h.blog.pageStore.GetPageByID(t.Context(), original.ID); err != nil || saved != original {
		t.Fatal("rejected update changed storage", saved, err)
	}
	if pages, err := h.blog.pageStore.ListPages(t.Context()); err != nil || len(pages) != 1 {
		t.Fatal("rejected creation changed storage", pages, err)
	}
}

// TestPageRequestLimits keeps malformed and over-limit bodies out of storage.
func TestPageRequestLimits(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, tt := range []struct {
		body   string
		status int
	}{
		{"title=Valid&slug=invalid&content=%ZZ", http.StatusBadRequest},
		{"title=Valid&slug=invalid&content=" + strings.Repeat("x", int(routing.MiB)), http.StatusRequestEntityTooLarge},
	} {
		for _, hx := range []bool{false, true} {
			r := httptest.NewRequest("POST", "https://blog.example/pages", strings.NewReader(tt.body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if hx {
				r.Header.Set("HX-Request", "true")
			}
			for _, cookie := range h.cookies {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			h.app.Handler.ServeHTTP(w, r)
			requireStatus(t, w, tt.status)
		}
	}
	if pages, err := h.blog.pageStore.ListPages(t.Context()); err != nil || len(pages) != 1 {
		t.Fatal("rejected request changed storage", pages, err)
	}
	fields := pageFields("boundary", true)
	fields.Set("content", strings.Repeat("x", maxMarkdownBytes))
	requireStatus(t, h.request("POST", "/pages", fields, false), http.StatusSeeOther)
	if saved, err := h.blog.pageStore.GetPageBySlug(t.Context(), "boundary", true); err != nil || saved.Markdown != fields.Get("content") {
		t.Fatal("valid boundary content was not saved intact", err)
	}
}

// unavailablePages exercises storage errors without exposing implementation details.
type unavailablePages struct{}

func (unavailablePages) ListPages(context.Context) ([]Page, error) {
	return nil, errors.New("private storage failure")
}
func (unavailablePages) GetPageBySlug(context.Context, string, bool) (Page, error) {
	return Page{}, errors.New("private storage failure")
}
func (unavailablePages) GetPageByID(context.Context, string) (Page, error) {
	return Page{}, errors.New("private storage failure")
}
func (unavailablePages) SavePage(context.Context, Page) (Page, error) {
	return Page{}, errors.New("private storage failure")
}
func (unavailablePages) DeletePage(context.Context, string) error {
	return errors.New("private storage failure")
}

// TestPageFailures preserves drafts on failed saves and denies a different verified user.
func TestPageFailures(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	h.blog.pageStore = unavailablePages{}
	for _, hx := range []bool{false, true} {
		for _, path := range []string{"/about", "/page/other", "/pages", "/pages/update/id", "/pages/delete/id"} {
			w := h.request("GET", path, nil, hx)
			requireStatus(t, w, http.StatusInternalServerError)
			if strings.Contains(w.Body.String(), "private storage failure") {
				t.Fatal("storage detail leaked")
			}
		}
		for _, path := range []string{"/pages", "/pages/id", "/pages/delete/id"} {
			w := h.request("POST", path, pageFields("unsaved", true), hx)
			requireStatus(t, w, http.StatusInternalServerError)
			if w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" || strings.Contains(w.Body.String(), "private storage failure") {
				t.Fatal("incorrect failed-write response")
			}
			if path != "/pages/delete/id" {
				if hx && !strings.Contains(w.Body.String(), "Unable to save this page") {
					t.Fatal("missing save feedback")
				}
				if !hx && (!strings.Contains(w.Body.String(), "Page unsaved") || !strings.Contains(w.Body.String(), "A **useful** page.")) {
					t.Fatal("failed save lost draft")
				}
			}
		}
	}
	for _, method := range []string{"GET", "POST"} {
		r := user.AddUserToRequestContext(httptest.NewRequest(method, "/pages", nil), &user.User{Email: "other@example.com", Verified: true})
		w := httptest.NewRecorder()
		h.app.Web.ServeHTTP(w, r)
		requireStatus(t, w, http.StatusForbidden)
	}
}

// TestPageStorage checks empty setup, persistence, cancellation, and concurrent uniqueness.
func TestPageStorage(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "pages.db?_journal=WAL"
	directory := t.TempDir()
	store, closeStore, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	if pages, err := store.ListPages(t.Context()); err != nil || len(pages) != 0 {
		t.Fatal("unexpected initial pages", pages, err)
	}
	if _, err := store.GetPageBySlug(t.Context(), "about", false); !errors.Is(err, errNotFound) {
		t.Fatal("missing page", err)
	}
	if _, err := store.SavePage(t.Context(), Page{ID: "missing"}); !errors.Is(err, errNotFound) {
		t.Fatal("updated missing page", err)
	}

	p, err := store.SavePage(t.Context(), Page{Slug: "about", Title: "About", Markdown: "Biography", Published: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.URL() != "/about" || (Page{Slug: "contact"}).URL() != "/page/contact" {
		t.Fatal("incorrect page URL")
	}
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.SavePage(t.Context(), Page{Slug: "concurrent", Title: "A draft"})
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
		t.Fatal("concurrent slug writes", winners)
	}
	if _, err := store.GetPageBySlug(t.Context(), "concurrent", false); !errors.Is(err, errNotFound) {
		t.Fatal("public draft read", err)
	}
	if pages, err := store.ListPages(t.Context()); err != nil || len(pages) != 2 || pages[0].Slug != "concurrent" || pages[1].Markdown != "" {
		t.Fatal("incorrect management listing", pages, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.SavePage(ctx, p); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored write cancellation", err)
	}
	if _, err := store.ListPages(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored list cancellation", err)
	}
	if _, err := store.GetPageByID(ctx, p.ID); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored read cancellation", err)
	}
	if err := store.DeletePage(ctx, p.ID); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored delete cancellation", err)
	}
	if err := closeStore(); err != nil {
		t.Fatal(err)
	}
	store, closeAgain, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAgain()
	if saved, err := store.GetPageByID(t.Context(), p.ID); err != nil || saved != p {
		t.Fatal("page not preserved", saved, err)
	}
	if err := store.DeletePage(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeletePage(t.Context(), p.ID); !errors.Is(err, errNotFound) {
		t.Fatal("delete missing page", err)
	}
	if err := closeAgain(); err != nil {
		t.Fatal(err)
	}
	store, closeThird, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeThird()
	if _, err := store.GetPageBySlug(t.Context(), "about", true); !errors.Is(err, errNotFound) {
		t.Fatal("startup resurrected deleted About", err)
	}
}
