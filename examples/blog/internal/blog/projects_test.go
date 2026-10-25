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

func projectFields(slug string, published bool) url.Values {
	v := url.Values{"slug": {slug}, "title": {"Project " + slug}, "summary": {"A short description"}, "content": {"# Details\n\nProject content"}, "displayOrder": {"10"}}
	if published {
		v.Set("published", "on")
	}
	return v
}

// TestProjectsHTTP exercises draft privacy, edits, publication, and image removal
// through ordinary and HTMX requests using real storage and authentication.
func TestProjectsHTTP(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "HTMX"}[hx], func(t *testing.T) {
			h := newHarness(t)
			for _, path := range []string{"/project/create", "/project/update/missing", "/project/delete/missing"} {
				requireStatus(t, h.request("GET", path, nil, hx), http.StatusForbidden)
			}
			for _, path := range []string{"/project", "/project/missing", "/project/delete/missing"} {
				requireStatus(t, h.request("POST", path, projectFields("blocked", true), hx), http.StatusForbidden)
			}
			h.register(t)
			h.login(t)
			requireStatus(t, h.request("POST", "/project", projectFields("unverified", true), hx), http.StatusForbidden)
			h.verify(t)
			redirectStatus, header := http.StatusSeeOther, "Location"
			if hx {
				redirectStatus, header = http.StatusOK, "HX-Redirect"
			}
			fields := projectFields("draft", false)
			w := h.request("POST", "/project", fields, hx)
			requireStatus(t, w, redirectStatus)
			if w.Header().Get(header) != "/project/s/draft" {
				t.Fatal("wrong project redirect", w.Header())
			}
			p, err := h.blog.projectStore.GetProjectBySlug(t.Context(), "draft", true)
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
			requireStatus(t, h.request("GET", "/project/s/draft", nil, hx), http.StatusNotFound)
			if strings.Contains(h.request("GET", "/projects", nil, hx).Body.String(), "/project/s/draft") {
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
			requireStatus(t, h.request("POST", "/project/"+p.ID, fields, hx), redirectStatus)
			h.cookies = nil
			w = h.request("GET", "/projects", nil, hx)
			if !strings.Contains(w.Body.String(), "/images/"+imageID) || !strings.Contains(w.Body.String(), "/project/s/published") {
				t.Fatal("published card missing", w.Body)
			}
			w = h.request("GET", "/project/s/published", nil, hx)
			requireStatus(t, w, http.StatusOK)
			if strings.Contains(w.Body.String(), imageID) || !strings.Contains(w.Body.String(), "<h1>Details</h1>") {
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
			requireStatus(t, h.request("POST", "/project/"+p.ID, fields, hx), redirectStatus)
			if saved, err := h.blog.projectStore.GetProjectByID(t.Context(), p.ID); err != nil || saved.PreviewImage != replacement || saved.Markdown != fields.Get("content") {
				t.Fatal("preview replacement failed", saved, err)
			}
			fields.Set("previewImage", "")
			fields.Del("published")
			requireStatus(t, h.request("POST", "/project/"+p.ID, fields, hx), redirectStatus)
			updated, err := h.blog.projectStore.GetProjectByID(t.Context(), p.ID)
			if err != nil || updated.Published || updated.PreviewImage != "" {
				t.Fatal("unpublish or image removal failed", updated, err)
			}
			h.cookies = nil
			requireStatus(t, h.request("GET", "/project/s/published", nil, hx), http.StatusNotFound)
			h.cookies = cookies
			requireStatus(t, h.request("GET", "/project/delete/"+p.ID, nil, hx), http.StatusOK)
			requireStatus(t, h.request("POST", "/project/delete/"+p.ID, nil, hx), redirectStatus)
			requireStatus(t, h.request("POST", "/project/delete/"+p.ID, nil, hx), http.StatusNotFound)
			requireStatus(t, h.request("POST", "/project/"+p.ID, fields, hx), http.StatusNotFound)
		})
	}
}

// TestProjectValidation keeps invalid input out of storage and preserves editor errors.
func TestProjectValidation(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, field := range []string{"title", "summary", "content", "slug", "displayOrder", "previewImage", "published"} {
		t.Run(field, func(t *testing.T) {
			for _, hx := range []bool{false, true} {
				fields := projectFields("valid", true)
				bad := map[string]string{"title": "", "summary": "", "content": strings.Repeat("x", maxMarkdownBytes+1), "slug": "../bad", "displayOrder": "-1", "previewImage": "../../private", "published": "maybe"}
				fields.Set(field, bad[field])
				w := h.request("POST", "/project", fields, hx)
				if w.Code < 400 || w.Code >= 500 || w.Header().Get("HX-Redirect") != "" {
					t.Fatalf("invalid %s: %d %s", field, w.Code, w.Body)
				}
				if !hx && !strings.Contains(w.Body.String(), `id="project-form"`) {
					t.Fatal("ordinary error lost editor")
				}
			}
		})
	}
	if projects, err := h.blog.projectStore.ListProjects(t.Context(), true); err != nil || len(projects) != 0 {
		t.Fatal("invalid input persisted", projects, err)
	}
	fields := projectFields("duplicate", true)
	requireStatus(t, h.request("POST", "/project", fields, false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/project", fields, true), http.StatusConflict)
	fields.Set("slug", "escaped")
	fields.Set("title", `<script>alert(1)</script>`)
	fields.Set("content", `<script>alert(1)</script>`)
	requireStatus(t, h.request("POST", "/project", fields, false), http.StatusSeeOther)
	if strings.Contains(h.request("GET", "/project/s/escaped", nil, false).Body.String(), `<script>alert(1)</script>`) {
		t.Fatal("project content was not escaped")
	}
	h.blog.images = failingImageStore{}
	fields = projectFields("image-failure", true)
	fields.Set("previewImage", "image")
	w := h.request("POST", "/project", fields, true)
	requireStatus(t, w, http.StatusInternalServerError)
	if w.Header().Get("HX-Redirect") != "" {
		t.Fatal("failed image lookup produced success")
	}
}

// TestProjectStorage checks stable ordering, unique slugs, persistence, and cancellation.
func TestProjectStorage(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "blog.db?_journal=WAL"
	directory := t.TempDir()
	store, closeStore, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore() })
	firstOrder, lastOrder := 0, 20
	for _, p := range []Project{
		{Slug: "last", Title: "A", DisplayOrder: &lastOrder, Published: true, Markdown: "body"},
		{Slug: "first", Title: "B", DisplayOrder: &firstOrder, Published: true},
		{Slug: "draft", Title: "C", DisplayOrder: &firstOrder},
	} {
		if _, err := store.SaveProject(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := store.ListProjects(t.Context(), false)
	if err != nil || len(projects) != 2 || projects[0].Slug != "first" || projects[1].Slug != "last" || projects[1].Markdown != "" {
		t.Fatal("incorrect project listing", projects, err)
	}
	if _, err := store.GetProjectBySlug(t.Context(), "draft", false); !errors.Is(err, errNotFound) {
		t.Fatal("draft visible", err)
	}
	if _, err := store.SaveProject(t.Context(), Project{ID: "missing"}); !errors.Is(err, errNotFound) {
		t.Fatal("update inserted missing project", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.SaveProject(t.Context(), Project{Slug: "concurrent"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, errProjectSlugTaken) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d duplicate slug winners", winners)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ListProjects(ctx, true); !errors.Is(err, context.Canceled) {
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
	if p, err := store.GetProjectBySlug(t.Context(), "last", false); err != nil || p.Markdown != "body" {
		t.Fatal("project not persisted", p, err)
	}
}

// TestProjectSortOrder checks explicit zero, matching orders, unordered projects,
// stable timestamp ties, and the update time assigned when a project is saved.
func TestProjectSortOrder(t *testing.T) {
	h := newHarness(t)
	store := h.blog.projectStore.(*sqliteRepository)
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
		p, err := store.SaveProject(t.Context(), Project{Slug: item.slug, DisplayOrder: item.order, Published: true})
		if err != nil {
			t.Fatal(err)
		}
		if p.Updated.IsZero() {
			t.Fatal("creation did not record update time")
		}
		if _, err := store.db.ExecContext(t.Context(), "UPDATE blog_projects SET updated=? WHERE id=?", base.Add(item.age).UnixNano(), p.ID); err != nil {
			t.Fatal(err)
		}
	}
	assertOrder := func(want string) {
		t.Helper()
		projects, err := store.ListProjects(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		var slugs []string
		for _, p := range projects {
			slugs = append(slugs, p.Slug)
		}
		if got := strings.Join(slugs, ","); got != want {
			t.Fatalf("order=%s, want %s", got, want)
		}
	}
	assertOrder("zero,ordered-new,ordered-old,unordered-new,unordered-old")
	p, err := store.GetProjectBySlug(t.Context(), "ordered-old", true)
	if err != nil {
		t.Fatal(err)
	}
	oldUpdated := p.Updated
	p, err = store.SaveProject(t.Context(), p)
	if err != nil || !p.Updated.After(oldUpdated) {
		t.Fatal("save did not update timestamp", p, err)
	}
	assertOrder("zero,ordered-old,ordered-new,unordered-new,unordered-old")
	p.DisplayOrder = nil
	if _, err := store.SaveProject(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	assertOrder("zero,ordered-new,ordered-old,unordered-new,unordered-old")
	// Equal timestamps use ID, not title or unstable row ordering.
	if _, err := store.db.ExecContext(t.Context(), "UPDATE blog_projects SET updated=?, id=slug WHERE display_order IS NULL", base.UnixNano()); err != nil {
		t.Fatal(err)
	}
	assertOrder("zero,ordered-new,ordered-old,unordered-new,unordered-old")
}

// TestProjectOptionalOrder checks empty form values, explicit zero, clearing an
// existing order, invalid numbers, and rendering the field without a nil placeholder.
func TestProjectOptionalOrder(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	fields := projectFields("optional", true)
	fields.Del("displayOrder")
	requireStatus(t, h.request("POST", "/project", fields, false), http.StatusSeeOther)
	p, err := h.blog.projectStore.GetProjectBySlug(t.Context(), "optional", true)
	if err != nil || p.DisplayOrder != nil {
		t.Fatal("missing order must be nil", p, err)
	}
	for _, value := range []string{"0", "1000000", ""} {
		fields.Set("displayOrder", value)
		requireStatus(t, h.request("POST", "/project/"+p.ID, fields, false), http.StatusSeeOther)
		w := h.request("GET", "/project/update/"+p.ID, nil, false)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), `name="displayOrder" type="number" min="0" max="1000000" value="`+value+`"`) {
			t.Fatal("order field did not round-trip", w.Body)
		}
	}
	for _, invalid := range []string{"-1", "1.5", "1000001", "word", "9999999999999999999999999"} {
		fields.Set("displayOrder", invalid)
		requireStatus(t, h.request("POST", "/project/"+p.ID, fields, true), http.StatusUnprocessableEntity)
	}
	p, err = h.blog.projectStore.GetProjectByID(t.Context(), p.ID)
	if err != nil || p.DisplayOrder != nil {
		t.Fatal("cleared order was not persisted", p, err)
	}
}

type failingProjects struct{ projectRepository }

func (failingProjects) SaveProject(context.Context, Project) (Project, error) {
	return Project{}, errors.New("private storage detail")
}
func (failingProjects) ListProjects(context.Context, bool) ([]Project, error) {
	return nil, errors.New("private storage detail")
}
func (failingProjects) DeleteProject(context.Context, string) error {
	return errors.New("private storage detail")
}
func (failingProjects) GetProjectBySlug(context.Context, string, bool) (Project, error) {
	return Project{}, errors.New("private storage detail")
}
func (failingProjects) GetProjectByID(context.Context, string) (Project, error) {
	return Project{}, errors.New("private storage detail")
}

// TestProjectStorageFailures ensures failures do not produce successful redirects.
func TestProjectStorageFailures(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	h.blog.projectStore = failingProjects{h.blog.projectStore}
	for _, route := range []struct{ method, path string }{
		{"POST", "/project"}, {"POST", "/project/delete/missing"}, {"GET", "/projects"},
		{"GET", "/project/s/missing"}, {"GET", "/project/update/missing"}, {"GET", "/project/delete/missing"},
	} {
		w := h.request(route.method, route.path, projectFields("failed", true), true)
		requireStatus(t, w, http.StatusInternalServerError)
		if strings.Contains(w.Body.String(), "private storage detail") || w.Header().Get("HX-Redirect") != "" {
			t.Fatal("storage failure leaked or redirected", w.Body)
		}
	}
}

// TestProjectOtherAccount verifies that a signed-in account is not sufficient for
// draft access or publishing, even if that account has verified its email.
func TestProjectOtherAccount(t *testing.T) {
	h := newHarness(t)
	p, err := h.blog.projectStore.SaveProject(t.Context(), Project{Slug: "private", Title: "Private draft"})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/project/s/private", http.StatusNotFound},
		{"GET", "/project/update/" + p.ID, http.StatusForbidden},
		{"POST", "/project/" + p.ID, http.StatusForbidden},
		{"POST", "/project/delete/" + p.ID, http.StatusForbidden},
	} {
		r := user.AddUserToRequestContext(httptest.NewRequest(route.method, route.path, nil), &user.User{Email: "someone@example.com", Verified: true})
		w := httptest.NewRecorder()
		// Supply identity directly to isolate module authorization from the login allowlist.
		h.app.Web.ServeHTTP(w, r)
		requireStatus(t, w, route.status)
	}
}
