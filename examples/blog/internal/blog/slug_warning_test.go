package blog

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestSlugWarningHistory covers full-page and HTMX editors, including ordinary
// validation failures that must retain the saved slug rather than submitted input.
func TestSlugWarningHistory(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, kind := range []string{"post", "page"} {
		for _, state := range []string{"new", "draft", "published", "unpublished"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				slug := kind + "-" + state
				editor, action := "/post/create", "/post"
				if kind == "page" {
					editor, action = "/pages/create", "/pages"
				}
				if state != "new" {
					if kind == "post" {
						p, err := h.blog.posts.Save(t.Context(), Post{Slug: slug, Title: "Title", Markdown: "Body", Draft: state == "draft"})
						if err != nil {
							t.Fatal(err)
						}
						if state == "unpublished" {
							if _, err := h.blog.posts.SetPublished(t.Context(), p.ID, false); err != nil {
								t.Fatal(err)
							}
						}
						editor, action = "/post/update/"+p.ID, "/post/"+p.ID
					} else {
						p, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: slug, Title: "Title", Published: state != "draft"})
						if err != nil {
							t.Fatal(err)
						}
						if state == "unpublished" {
							p.Published, p.EverPublished = false, false
							if _, err := h.blog.pageStore.SavePage(t.Context(), p); err != nil {
								t.Fatal(err)
							}
						}
						editor, action = "/pages/update/"+p.ID, "/pages/"+p.ID
					}
				}
				check := func(body string) {
					t.Helper()
					want := state == "published" || state == "unpublished"
					if strings.Contains(body, "data-published-slug=") != want ||
						(want && !strings.Contains(body, `data-published-slug="`+slug+`"`)) ||
						strings.Contains(body, "Changing or clearing this slug can break existing links.") != want {
						t.Fatal("warning does not match stored publication history")
					}
				}
				for _, hx := range []bool{false, true} {
					response := h.request("GET", editor, nil, hx)
					requireStatus(t, response, http.StatusOK)
					check(response.Body.String())
				}
				response := h.request("POST", action, url.Values{"slug": {"changed-url"}}, false)
				requireStatus(t, response, http.StatusUnprocessableEntity)
				check(response.Body.String())
				if !strings.Contains(response.Body.String(), `value="changed-url"`) {
					t.Fatal("validation failure lost the submitted slug")
				}
			})
		}
	}
}

// TestPagePublicationHistoryUpgrade verifies backfill, sticky history, and reopening.
func TestPagePublicationHistoryUpgrade(t *testing.T) {
	directory := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(directory, "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE blog_pages (
id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL, markdown TEXT NOT NULL,
published INTEGER NOT NULL, parent_id TEXT REFERENCES blog_pages(id), summary TEXT NOT NULL,
preview_image TEXT NOT NULL, children_display TEXT NOT NULL, display_order INTEGER, updated INTEGER NOT NULL);
INSERT INTO blog_pages VALUES ('live', 'live', 'Live', '', 1, NULL, '', '', 'hidden', NULL, 1);
INSERT INTO blog_pages VALUES ('draft', 'draft', 'Draft', '', 0, NULL, '', '', 'hidden', NULL, 1);`)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "pages.db"
	for iteration := range 2 {
		store, closeStore, err := openRepository(t.Context(), settings, directory)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeStore() })
		live, err := store.GetPageByID(t.Context(), "live", true)
		if err != nil || !live.EverPublished || live.Published != (iteration == 0) {
			t.Fatal("publication history did not survive upgrade or reopen", live, err)
		}
		draft, err := store.GetPageBySlug(t.Context(), "draft", true)
		if err != nil || draft.EverPublished {
			t.Fatal("upgrade invented draft history", draft, err)
		}
		live.Published, live.EverPublished = false, false
		live, err = store.SavePage(t.Context(), live)
		if err != nil || !live.EverPublished || live.Published {
			t.Fatal("unpublishing discarded history", live, err)
		}
		if err := closeStore(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPageFirstPublicationHistory ignores caller-supplied history and retains it
// through subsequent saves. Failed writes must not record a publication.
func TestPageFirstPublicationHistory(t *testing.T) {
	h := newHarness(t)
	p, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "never-live", Title: "Draft", EverPublished: true})
	if err != nil || p.EverPublished {
		t.Fatal("new draft trusted caller-supplied history", p, err)
	}
	p.Published, p.Slug = true, "about"
	if _, err := h.blog.pageStore.SavePage(t.Context(), p); err != errPageSlugTaken {
		t.Fatal("expected slug conflict", err)
	}
	p, err = h.blog.pageStore.GetPageByID(t.Context(), p.ID, true)
	if err != nil || p.EverPublished || p.Published {
		t.Fatal("failed write changed history", p, err)
	}
	p.Published = true
	p, err = h.blog.pageStore.SavePage(t.Context(), p)
	if err != nil || !p.EverPublished {
		t.Fatal("first publication was not recorded", p, err)
	}
	p.Published, p.EverPublished = false, false
	p, err = h.blog.pageStore.SavePage(t.Context(), p)
	if err != nil || !p.EverPublished {
		t.Fatal("subsequent draft save discarded history", p, err)
	}
	var writers sync.WaitGroup
	for _, published := range []bool{true, false, true, false} {
		writers.Go(func() {
			update := p
			update.Published, update.EverPublished = published, false
			if _, err := h.blog.pageStore.SavePage(t.Context(), update); err != nil && !errors.Is(err, errEditConflict) {
				t.Error(err)
			}
		})
	}
	writers.Wait()
	stored, err := h.blog.pageStore.GetPageByID(t.Context(), p.ID, true)
	if err != nil || !stored.EverPublished {
		t.Fatal("concurrent updates discarded history", stored, err)
	}
}
