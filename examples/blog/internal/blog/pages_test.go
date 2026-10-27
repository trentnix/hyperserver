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
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// TestPageManagement checks empty-state controls and accurate editor guidance.
func TestPageManagement(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	pages, err := h.blog.pageStore.ListPages(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range pages {
		if err := h.blog.pageStore.DeletePage(t.Context(), page.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, hx := range []bool{false, true} {
		w := h.request("GET", "/pages", nil, hx)
		requireStatus(t, w, http.StatusOK)
		for _, want := range []string{`href="/pages/create"`, "No pages yet. Create a page to get started."} {
			if !strings.Contains(w.Body.String(), want) {
				t.Fatalf("empty management screen missing %q", want)
			}
		}
		w = h.request("GET", "/pages/create", nil, hx)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), "Navigation shows top-level pages and their immediate children.") {
			t.Fatal("editor does not explain the navigation depth limit")
		}
	}
}

// TestPageNavigationTitles keeps both menus tied to editable, escaped content.
func TestPageNavigationTitles(t *testing.T) {
	h := newHarness(t)
	page, err := h.blog.pageStore.GetPageBySlug(t.Context(), "about", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Our story", "Company <history>"} {
		page.Title = title
		if page, err = h.blog.pageStore.SavePage(t.Context(), page); err != nil {
			t.Fatal(err)
		}
		body := h.request("GET", "/", nil, false).Body.String()
		if strings.Count(body, `href="/about">`+html.EscapeString(title)+`</a>`) != 2 {
			t.Fatal("navigation did not use the stored title", title)
		}
	}
}

// TestPageDisplayOrderInput preserves raw input on ordinary validation failures.
func TestPageDisplayOrderInput(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	page, err := h.blog.pageStore.GetPageBySlug(t.Context(), "about", true)
	if err != nil {
		t.Fatal(err)
	}
	page.DisplayOrder = new(42)
	page, err = h.blog.pageStore.SavePage(t.Context(), page)
	if err != nil {
		t.Fatal(err)
	}
	input := regexp.MustCompile(`<input id="page-order"[^>]+>`)
	for _, value := range []string{"-1", "1000001", "1.5", "not-a-number", strings.Repeat("9", 40), `"><script>`, "", "0", "0005", " 12 "} {
		t.Run(value, func(t *testing.T) {
			fields := pageFields(page.Slug, true)
			fields.Set("displayOrder", value)
			if value == "" || value == "0" || value == "0005" || value == " 12 " {
				fields.Set("title", "") // An unrelated failure must preserve valid input too.
			}
			w := h.submitEditor(t, "/pages/"+page.ID, fields, false)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			field := input.FindString(w.Body.String())
			if !strings.Contains(field, `value="`+html.EscapeString(value)+`"`) || strings.Contains(field, `type="number"`) {
				t.Fatal("failed submission lost display order", field)
			}
		})
	}
	stored, err := h.blog.pageStore.GetPageByID(t.Context(), page.ID, true)
	if err != nil || !reflect.DeepEqual(stored, page) {
		t.Fatal("invalid submission changed stored page", err)
	}
	for _, order := range []*int{nil, new(0), new(42)} {
		page.DisplayOrder = order
		if page, err = h.blog.pageStore.SavePage(t.Context(), page); err != nil {
			t.Fatal(err)
		}
		body := h.request("GET", "/pages/update/"+page.ID, nil, false).Body.String()
		want := ""
		if order != nil {
			want = strconv.Itoa(*order)
		}
		if !strings.Contains(input.FindString(body), `value="`+want+`"`) {
			t.Fatal("editor did not show stored display order", want)
		}
	}
}

// pageFields supplies valid form values, leaving publication unchecked for drafts.
func pageFields(slug string, published bool) url.Values {
	values := url.Values{"slug": {slug}, "title": {"Page " + slug}, "content": {"A **useful** page."}}
	if published {
		values.Set("published", "on")
	}
	return values
}

// TestAboutNavigation keeps both menus in step with the published About page.
func TestAboutNavigation(t *testing.T) {
	for _, hx := range []bool{false, true} {
		h := newHarness(t)
		about, err := h.blog.pageStore.GetPageBySlug(t.Context(), "about", true)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.blog.pageStore.DeletePage(t.Context(), about.ID); err != nil {
			t.Fatal(err)
		}
		h.register(t)
		h.verify(t)
		h.login(t)
		cookies := h.cookies
		check := func(visible bool) {
			t.Helper()
			want := 0
			if visible {
				want = 2 // The desktop profile and mobile menu.
			}
			for _, signedIn := range []bool{false, true} {
				h.cookies = nil
				if signedIn {
					h.cookies = cookies
				}
				w := h.request("GET", "/", nil, false)
				requireStatus(t, w, http.StatusOK)
				if got := strings.Count(w.Body.String(), `href="/about"`); got != want {
					t.Fatalf("signedIn=%v: About links=%d, want %d", signedIn, got, want)
				}
			}
			h.cookies = cookies
		}
		check(false)
		status := http.StatusSeeOther
		if hx {
			status = http.StatusOK
		}
		requireStatus(t, h.request("POST", "/pages", pageFields("about", false), hx), status)
		check(false)
		about, err = h.blog.pageStore.GetPageBySlug(t.Context(), "about", true)
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range []struct {
			slug      string
			published bool
		}{
			{"about", true},
			{"about", false},
			{"about", true},
			{"other-page", true},
			{"about", true},
		} {
			requireStatus(t, h.submitEditor(t, "/pages/"+about.ID, pageFields(change.slug, change.published), hx), status)
			check(change.slug == "about" && change.published)
		}
		requireStatus(t, h.request("POST", "/pages/delete/"+about.ID, nil, hx), status)
		check(false)
	}
}

// TestSiteNavigation exposes published metadata only and preserves project order.
func TestSiteNavigation(t *testing.T) {
	h := newHarness(t)
	for _, page := range []Page{
		{Slug: "contact", Title: "Contact <us>", Published: true},
		{Slug: "private-page", Title: "Private page"},
	} {
		if _, err := h.blog.pageStore.SavePage(t.Context(), page); err != nil {
			t.Fatal(err)
		}
	}
	for _, project := range []Page{
		{Slug: "last", ParentID: h.projectsID, Title: "Last project", Published: true, DisplayOrder: new(20)},
		{Slug: "first", ParentID: h.projectsID, Title: "First <project>", Published: true, DisplayOrder: new(10)},
		{Slug: "private-project", ParentID: h.projectsID, Title: "Private project"},
	} {
		if _, err := h.blog.pageStore.SavePage(t.Context(), project); err != nil {
			t.Fatal(err)
		}
	}
	for _, signedIn := range []bool{false, true} {
		if signedIn {
			h.register(t)
			h.verify(t)
			h.login(t)
		}
		body := h.request("GET", "/", nil, false).Body.String()
		if strings.Count(body, `href="/page/contact"`) != 2 || strings.Count(body, `href="/page/first"`) != 2 {
			t.Fatal("navigation is missing page or project links")
		}
		if !strings.Contains(body, "Contact &lt;us&gt;") || !strings.Contains(body, "First &lt;project&gt;") {
			t.Fatal("navigation titles were not escaped")
		}
		if strings.Contains(body, "private-page") || strings.Contains(body, "private-project") {
			t.Fatal("drafts appeared in public navigation")
		}
		if strings.Index(body, `href="/page/first"`) > strings.Index(body, `href="/page/last"`) {
			t.Fatal("project navigation ignored display order")
		}
	}
	h.blog.pageStore = unavailablePages{}
	body := h.request("GET", "/", nil, false).Body.String()
	if strings.Contains(body, `class="child-links"`) || strings.Contains(body, "private storage failure") || strings.Contains(body, `href="/page/contact"`) || strings.Contains(body, `href="/projects"`) {
		t.Fatal("project lookup failure broke page navigation or leaked details")
	}
}

type unavailablePageNavigation struct{ pageRepository }

func (unavailablePageNavigation) ListPages(context.Context, bool) ([]Page, error) {
	return []Page{{Slug: "about", Published: true}}, errors.New("private navigation storage failure")
}

// TestAboutNavigationFailure hides an uncertain link without breaking the page.
func TestAboutNavigationFailure(t *testing.T) {
	h := newHarness(t)
	h.blog.pageStore = unavailablePageNavigation{h.blog.pageStore}
	for _, path := range []string{"/", loginURL, "/missing"} {
		w := h.request("GET", path, nil, false)
		want := http.StatusOK
		if path == "/missing" {
			want = http.StatusNotFound
		}
		requireStatus(t, w, want)
		if strings.Contains(w.Body.String(), `href="/about"`) || strings.Contains(w.Body.String(), "private navigation") {
			t.Fatal("navigation exposed an uncertain link or private error")
		}
	}
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
			requireStatus(t, h.submitEditor(t, "/pages/"+p.ID, fields, hx), status)
			h.cookies = nil
			requireStatus(t, h.request("GET", "/page/draft", nil, hx), http.StatusNotFound)
			w = h.request("GET", "/page/published", nil, hx)
			requireStatus(t, w, http.StatusOK)
			if !strings.Contains(w.Body.String(), "<strong>useful</strong>") || strings.Contains(w.Body.String(), "<script>alert(1)</script>") || strings.Contains(w.Body.String(), "Edit page") {
				t.Fatal("unsafe or incorrect public page", w.Body)
			}
			for _, path := range []string{"/rss", "/posts/recent", "/posts"} {
				if strings.Contains(h.request("GET", path, nil, true).Body.String(), "Page draft") {
					t.Fatal("page leaked into post collections", path)
				}
			}
			if strings.Count(h.request("GET", "/projects", nil, false).Body.String(), `href="/page/published"`) != 2 {
				t.Fatal("published page missing from navigation")
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
			w = h.submitEditor(t, "/pages/"+about.ID, fields, hx)
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
			requireStatus(t, h.submitEditor(t, "/pages/"+about.ID, fields, hx), status)
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
		{"slug", "../bad"}, {"slug", "About"}, {"slug", strings.Repeat("a", 121)},
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
	if pages, err := h.blog.pageStore.ListPages(t.Context(), true); err != nil || len(pages) != 2 {
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
	requireStatus(t, h.submitEditor(t, "/pages/"+p.ID, fields, false), http.StatusConflict)
	if saved, err := h.blog.pageStore.GetPageByID(t.Context(), p.ID, true); err != nil || saved.Slug != "escaped" {
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
	if saved, err := h.blog.pageStore.GetPageByID(t.Context(), original.ID, true); err != nil || !reflect.DeepEqual(saved, original) {
		t.Fatal("rejected update changed storage", saved, err)
	}
	if pages, err := h.blog.pageStore.ListPages(t.Context(), true); err != nil || len(pages) != 2 {
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
	if pages, err := h.blog.pageStore.ListPages(t.Context(), true); err != nil || len(pages) != 2 {
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

func (unavailablePages) ListPages(context.Context, bool) ([]Page, error) {
	return nil, errors.New("private storage failure")
}
func (unavailablePages) GetPageBySlug(context.Context, string, bool) (Page, error) {
	return Page{}, errors.New("private storage failure")
}
func (unavailablePages) GetPageByID(context.Context, string, bool) (Page, error) {
	return Page{}, errors.New("private storage failure")
}
func (unavailablePages) SavePage(context.Context, Page) (Page, error) {
	return Page{}, errors.New("private storage failure")
}
func (unavailablePages) DeletePage(context.Context, string) error {
	return errors.New("private storage failure")
}

// TestPageDeleteWithChildren keeps conflict feedback in the confirmation flow.
func TestPageDeleteWithChildren(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	child, err := h.blog.pageStore.SavePage(t.Context(), Page{
		ParentID: h.projectsID, Slug: "child", Title: "Child", Published: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/pages/delete/" + h.projectsID
	for _, hx := range []bool{false, true} {
		confirmation := h.request("GET", path, nil, hx)
		requireStatus(t, confirmation, http.StatusOK)
		if !strings.Contains(confirmation.Body.String(), `hx-target="#page-delete-confirmation" hx-swap="outerHTML"`) {
			t.Fatal("delete form must target the confirmation contents")
		}

		w := h.request("POST", path, nil, hx)
		requireStatus(t, w, http.StatusConflict)
		body := w.Body.String()
		if !strings.Contains(body, "This page has subpages.") || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
			t.Fatal("blocked deletion must explain the conflict without redirecting")
		}
		if strings.Contains(body, `action="`+path+`"`) || strings.Contains(body, "Delete page?") || !strings.Contains(body, "Cannot delete page") {
			t.Fatal("blocked deletion must replace the confirmation with an explanation")
		}
		if hx {
			if strings.Contains(body, "<dialog") || strings.Contains(body, "<form") || strings.Contains(body, "<html") {
				t.Fatal("HTMX conflict must replace the dialog contents, not the dialog")
			}
			if !strings.Contains(body, `data-close-dialog autofocus>OK</button>`) {
				t.Fatal("blocked deletion must offer an OK button to dismiss the dialog")
			}
		} else if !strings.Contains(body, `href="/projects">OK</a>`) {
			t.Fatal("full-page error must link back to the page")
		}
		for _, id := range []string{h.projectsID, child.ID} {
			if _, err := h.blog.pageStore.GetPageByID(t.Context(), id, true); err != nil {
				t.Fatalf("blocked deletion changed stored pages: %v", err)
			}
		}
	}
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
			fields := pageFields("unsaved", true)
			fields.Set("parentID", "selected-parent")
			fields.Set("version", "1")
			w := h.request("POST", path, fields, hx)
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
				if !hx && !strings.Contains(w.Body.String(), `value="selected-parent" selected`) {
					t.Fatal("failed save lost parent selection")
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
	if pages, err := store.ListPages(t.Context(), true); err != nil || len(pages) != 0 {
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
	if pages, err := store.ListPages(t.Context(), true); err != nil || len(pages) != 2 || pages[0].Slug != "concurrent" || pages[1].Markdown != "" {
		t.Fatal("incorrect management listing", pages, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.SavePage(ctx, p); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored write cancellation", err)
	}
	if _, err := store.ListPages(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored list cancellation", err)
	}
	if _, err := store.GetPageByID(ctx, p.ID, true); !errors.Is(err, context.Canceled) {
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
	if saved, err := store.GetPageByID(t.Context(), p.ID, true); err != nil || saved != p {
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
