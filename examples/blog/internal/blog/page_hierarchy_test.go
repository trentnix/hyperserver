package blog

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestPageNavigationDepth limits menus, not page access or administrator tools.
func TestPageNavigationDepth(t *testing.T) {
	h := newHarness(t)
	child, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "child", Title: "Child", ParentID: h.projectsID, Published: true, ChildrenDisplay: "list"})
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "grandchild", Title: "Grandchild", ParentID: child.ID, Published: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, signedIn := range []bool{false, true} {
		if signedIn {
			h.register(t)
			h.verify(t)
			h.login(t)
		}
		body := h.request("GET", "/", nil, false).Body.String()
		if strings.Count(body, `href="`+child.URL()+`"`) != 2 || strings.Contains(body, grandchild.URL()) {
			t.Fatal("site navigation must include exactly two levels")
		}
		requireStatus(t, h.request("GET", grandchild.URL(), nil, false), http.StatusOK)
		body = h.request("GET", child.URL(), nil, true).Body.String()
		if !strings.Contains(body, `href="`+grandchild.URL()+`"`) {
			t.Fatal("navigation depth hid the child preview")
		}
	}
	if body := h.request("GET", "/pages", nil, true).Body.String(); !strings.Contains(body, grandchild.URL()) {
		t.Fatal("navigation depth hid a page from management")
	}
	if body := h.request("GET", "/pages/create", nil, true).Body.String(); !strings.Contains(body, `value="`+grandchild.ID+`"`) {
		t.Fatal("navigation depth hid a parent choice")
	}
}

// TestPageAutomaticSlug uses the title when the page's slug is omitted.
func TestPageAutomaticSlug(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	fields := pageFields("", true)
	requireStatus(t, h.request("POST", "/pages", fields, false), http.StatusSeeOther)
	pages, err := h.blog.pageStore.ListPages(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	var created Page
	for _, p := range pages {
		if p.Title == "Page" {
			created = p
		}
	}
	if created.Slug != "page" || created.ID == "" {
		t.Fatal("missing automatic slug", created)
	}
	requireStatus(t, h.submitEditor(t, "/pages/"+created.ID, fields, false), http.StatusSeeOther)
}

// TestPageHierarchy checks nested publication, reparenting, and protected deletion.
func TestPageHierarchy(t *testing.T) {
	h := newHarness(t)
	store := h.blog.pageStore
	root, err := store.GetPageByID(t.Context(), h.projectsID, true)
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.SavePage(t.Context(), Page{Slug: "child", Title: "Child", ParentID: root.ID, Published: true})
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := store.SavePage(t.Context(), Page{Slug: "grandchild", Title: "Grandchild", ParentID: child.ID, Published: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{root.ID, child.ID, grandchild.ID, "missing"} {
		invalid := root
		invalid.ParentID = parent
		if _, err := store.SavePage(t.Context(), invalid); !errors.Is(err, errPageParent) {
			t.Fatalf("parent %s: %v", parent, err)
		}
	}
	if err := store.DeletePage(t.Context(), root.ID); !errors.Is(err, errPageHasChildren) {
		t.Fatal("deleted parent", err)
	}
	if _, err := store.SavePage(t.Context(), Page{Slug: "orphan", ParentID: "missing"}); !errors.Is(err, errPageParent) {
		t.Fatal("created orphan", err)
	}

	root.Published = false
	if _, err := store.SavePage(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Page{root, child, grandchild} {
		requireStatus(t, h.request("GET", p.URL(), nil, false), http.StatusNotFound)
		if _, err := store.GetPageBySlug(t.Context(), p.Slug, true); err != nil {
			t.Fatal("admin lost draft branch", err)
		}
		if _, err := store.GetPageByID(t.Context(), p.ID, false); !errors.Is(err, errNotFound) {
			t.Fatal("public ID lookup exposed draft branch", err)
		}
		if _, err := store.GetPageByID(t.Context(), p.ID, true); err != nil {
			t.Fatal("admin ID lookup lost draft branch", err)
		}
	}
	pages, err := store.ListPages(t.Context(), false)
	if err != nil || len(pages) != 1 || pages[0].Slug != "about" {
		t.Fatal("public listing exposed draft branch", pages, err)
	}
	pages, err = store.ListPages(t.Context(), true)
	if err != nil || len(pages) != 4 {
		t.Fatal("admin listing lost draft branch", pages, err)
	}
	body := h.request("GET", "/", nil, false).Body.String()
	if strings.Contains(body, grandchild.URL()) || strings.Contains(body, child.URL()) || strings.Contains(body, root.URL()) {
		t.Fatal("draft branch leaked into navigation")
	}
	child.ParentID = ""
	if _, err := store.SavePage(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("GET", grandchild.URL(), nil, false), http.StatusOK)
	if _, err := store.GetPageByID(t.Context(), grandchild.ID, false); err != nil {
		t.Fatal("public ID lookup lost reparented branch", err)
	}
	pages, err = store.ListPages(t.Context(), false)
	if err != nil || len(pages) != 3 {
		t.Fatal("public listing lost reparented branch", pages, err)
	}
	if err := store.DeletePage(t.Context(), root.ID); err != nil {
		t.Fatal("empty parent cannot be deleted", err)
	}
}

// editedPageRepository reproduces an edit between the page and related-content reads.
type editedPageRepository struct {
	pageRepository
	afterLookup func()
}

func (s editedPageRepository) GetPageBySlug(ctx context.Context, slug string, includeDrafts bool) (Page, error) {
	p, err := s.pageRepository.GetPageBySlug(ctx, slug, includeDrafts)
	if err == nil {
		s.afterLookup()
	}
	return p, err
}

// TestPagePublicationDuringRead keeps newly private metadata out of public responses.
func TestPagePublicationDuringRead(t *testing.T) {
	for _, hx := range []bool{false, true} {
		for _, related := range []string{"parent", "children"} {
			t.Run(related+"/htmx="+strconv.FormatBool(hx), func(t *testing.T) {
				h := newHarness(t)
				store := h.blog.pageStore
				parent, err := store.GetPageByID(t.Context(), h.projectsID, true)
				if err != nil {
					t.Fatal(err)
				}
				child, err := store.SavePage(t.Context(), Page{Slug: "child", Title: "Child", ParentID: parent.ID, Published: true})
				if err != nil {
					t.Fatal(err)
				}
				h.blog.pageStore = editedPageRepository{pageRepository: store, afterLookup: func() {
					parent.Published = false
					parent.Title = "Private parent title"
					if _, err := store.SavePage(t.Context(), parent); err != nil {
						t.Fatal(err)
					}
					child.Summary = "Private child summary"
					if _, err := store.SavePage(t.Context(), child); err != nil {
						t.Fatal(err)
					}
				}}
				path, status := parent.URL(), http.StatusOK
				if related == "parent" {
					path, status = child.URL(), http.StatusNotFound
				}
				response := h.request("GET", path, nil, hx)
				requireStatus(t, response, status)
				if strings.Contains(response.Body.String(), "Private parent title") || strings.Contains(response.Body.String(), "Private child summary") {
					t.Fatal("related-content lookup exposed newly private metadata")
				}
			})
		}
	}
}

// TestConcurrentPageParents prevents two valid-looking edits from creating a cycle.
func TestConcurrentPageParents(t *testing.T) {
	h := newHarness(t)
	store := h.blog.pageStore
	a, err := store.SavePage(t.Context(), Page{Slug: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.SavePage(t.Context(), Page{Slug: "b"})
	if err != nil {
		t.Fatal(err)
	}
	a.ParentID, b.ParentID = b.ID, a.ID
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, p := range []Page{a, b} {
		wg.Go(func() {
			<-start
			_, err := store.SavePage(t.Context(), p)
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, errPageParent) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("accepted=%d rejected=%d", accepted, rejected)
	}
}

// TestPageChildDisplays applies all preview modes to an ordinary page, not a
// special Projects handler. Preview metadata never appears in the child's body.
func TestPageChildDisplays(t *testing.T) {
	for _, hx := range []bool{false, true} {
		h := newHarness(t)
		h.register(t)
		h.verify(t)
		h.login(t)
		parent, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "portfolio", Title: "Portfolio", Markdown: "Parent body", Published: true})
		if err != nil {
			t.Fatal(err)
		}
		child, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "child", Title: "Child", Summary: "Preview description", Markdown: "Child body", ParentID: parent.ID, Published: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"hidden", "list", "blocks"} {
			fields := pageFields(parent.Slug, true)
			fields.Set("childrenDisplay", mode)
			fields.Set("content", parent.Markdown)
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, h.submitEditor(t, "/pages/"+parent.ID, fields, hx), status)
			response := h.request("GET", parent.URL(), nil, hx)
			requireStatus(t, response, http.StatusOK)
			body := response.Body.String()
			if strings.Contains(body, "Preview description") != (mode != "hidden") {
				t.Fatal("wrong previews for", mode)
			}
			if strings.Contains(body, `class="page-grid"`) != (mode == "blocks") {
				t.Fatal("wrong block layout", mode)
			}
			if strings.Contains(body, `class="page-preview-list"`) != (mode == "list") {
				t.Fatal("wrong list layout", mode)
			}
			if !strings.Contains(body, "Parent body") {
				t.Fatal("lost parent content")
			}
			if !strings.Contains(h.request("GET", "/", nil, false).Body.String(), child.URL()) {
				t.Fatal("display setting hid navigation")
			}
		}
		body := h.request("GET", child.URL(), nil, true).Body.String()
		if strings.Contains(body, "Preview description") || strings.Contains(body, "project-default.svg") || !strings.Contains(body, "Child body") || !strings.Contains(body, parent.URL()) {
			t.Fatal("incorrect child content or backlink")
		}
		// Existing project detail URLs remain readable.
		requireStatus(t, h.request("GET", "/project/s/child", nil, hx), http.StatusOK)
		editor := h.request("GET", "/pages/update/"+parent.ID, nil, hx).Body.String()
		if strings.Contains(editor, `value="`+child.ID+`"`) {
			t.Fatal("parent chooser offered a descendant")
		}
		fields := pageFields(parent.Slug, true)
		fields.Set("parentID", child.ID)
		requireStatus(t, h.submitEditor(t, "/pages/"+parent.ID, fields, hx), http.StatusUnprocessableEntity)
		requireStatus(t, h.request("POST", "/pages/delete/"+parent.ID, nil, hx), http.StatusConflict)
		fields.Del("parentID")
		fields.Set("childrenDisplay", "invalid")
		requireStatus(t, h.submitEditor(t, "/pages/"+parent.ID, fields, hx), http.StatusUnprocessableEntity)
	}
}
