package blog

import (
	"errors"
	"html"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestEmptyComponentsCannotBeSaved(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := registeredBrowserHarness(t)
			h.login(t)
			for _, component := range []struct{ kind, field string }{
				{"photo-album", "album"}, {"file-downloads", "downloads"}, {"pdf-preview", "pdf"},
				{"link-preview", "link"}, {"video", "video"}, {"audio", "audio"},
			} {
				t.Run(component.kind, func(t *testing.T) {
					fields := url.Values{
						"title": {"Empty component review"}, "slug": {"empty-" + component.kind}, "content": {"Keep this writing."},
						"componentOrder": {`["` + component.kind + `"]`}, component.field: {""},
					}
					checkError := func(path string) {
						t.Helper()
						w := h.request("POST", path, fields, hx)
						requireStatus(t, w, http.StatusUnprocessableEntity)
						if !strings.Contains(w.Body.String(), `data-error-fields="`+component.field+`"`) || !strings.Contains(w.Body.String(), "remove") || w.Header().Get("HX-Trigger") != "" {
							t.Fatal("empty component must identify the field without reporting a save", w.Body.String())
						}
						if !hx {
							page := html.UnescapeString(w.Body.String())
							if !strings.Contains(page, `name="componentOrder" value='`+fields.Get("componentOrder")+`'`) && !strings.Contains(page, `name="componentOrder" value="`+fields.Get("componentOrder")+`"`) {
								t.Fatal("ordinary error lost the added empty component")
							}
							if !strings.Contains(page, fields.Get("content")) {
								t.Fatal("ordinary error lost submitted writing")
							}
						}
					}
					checkError("/post")
					if _, err := h.blog.posts.GetBySlug(t.Context(), fields.Get("slug"), true); !errors.Is(err, errNotFound) {
						t.Fatal("empty component created a post", err)
					}
					// Removing the component allows the same writing to save.
					fields.Set("componentOrder", `[]`)
					status := http.StatusSeeOther
					if hx {
						status = http.StatusOK
					}
					requireStatus(t, h.request("POST", "/post", fields, hx), status)
					post, err := h.blog.posts.GetBySlug(t.Context(), fields.Get("slug"), true)
					if err != nil || post.Markdown != fields.Get("content") {
						t.Fatal("removing an empty component must save only the body", post, err)
					}
					versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
					if err != nil {
						t.Fatal(err)
					}
					fields.Set("version", "1")
					fields.Set("content", "Keep these unsaved edits.")
					fields.Set("published", "on")
					fields.Set("componentOrder", `["`+component.kind+`"]`)
					checkError("/post/" + post.ID)
					current, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
					if err != nil || current != post {
						t.Fatal("empty component changed saved content or publication", current, err)
					}
					history, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
					if err != nil || !reflect.DeepEqual(history, versions) {
						t.Fatal("empty component created a revision", history, err)
					}
				})
			}
		})
	}
}

func TestEmptyComponentArraysCannotBeSaved(t *testing.T) {
	for _, input := range []componentInput{
		{"componentOrder": `["photo-album"]`, "album": " \n"},
		{"componentOrder": `["file-downloads"]`, "downloads": `[]`},
		{"componentOrder": `["file-downloads"]`, "downloads": `null`},
		{"componentOrder": `["pdf-preview"]`, "pdf": `[]`},
		{"componentOrder": `["pdf-preview"]`, "pdf": `null`},
		{"componentOrder": `["audio"]`},
	} {
		if _, err := joinPostComponents("", input); err == nil {
			t.Fatal("added empty component was silently dropped", input["componentOrder"])
		}
	}
}
