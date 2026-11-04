package blog

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// activeMarkup omits inert template contents when checking the rendered interface.
func activeMarkup(markup string) string {
	return regexp.MustCompile(`(?s)<template\b[^>]*>.*?</template>`).ReplaceAllString(markup, "")
}

func TestPageEditorErrorFields(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	page, err := h.blog.writePage(t.Context(), Page{Title: "Existing", Slug: "existing", ChildrenDisplay: "hidden"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, field, value, want string
		status                   int
	}{
		{"title", "title", " ", "title", http.StatusUnprocessableEntity},
		{"summary", "summary", strings.Repeat("s", 2001), "summary", http.StatusUnprocessableEntity},
		{"markdown", "content", strings.Repeat("x", maxMarkdownBytes+1), "content", http.StatusUnprocessableEntity},
		{"slug format", "slug", "Invalid!", "slug", http.StatusUnprocessableEntity},
		{"slug conflict", "slug", page.Slug, "slug", http.StatusConflict},
		{"tags", "tags", strings.Repeat("t", 51), "tags", http.StatusUnprocessableEntity},
		{"missing preview", "previewImage", strings.Repeat("a", 64) + ".png", "previewImage", http.StatusUnprocessableEntity},
		{"missing body image", "content", "![](/images/" + strings.Repeat("b", 64) + ".png)", "content", http.StatusUnprocessableEntity},
		{"subpages", "childrenDisplay", "invalid", "childrenDisplay", http.StatusUnprocessableEntity},
		{"order", "displayOrder", "1000001", "displayOrder", http.StatusUnprocessableEntity},
		{"parent", "parentID", "missing", "parentID", http.StatusUnprocessableEntity},
		{"publication", "published", "invalid", "published", http.StatusUnprocessableEntity},
		{"edit conflict", "version", "0", "", http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, hx := range []bool{false, true} {
				fields := url.Values{"title": {"A title"}, "content": {"Content"}, "slug": {"new-page"}}
				fields.Set(tc.field, tc.value)
				path := "/pages"
				if tc.name == "edit conflict" {
					path += "/" + page.ID
				}
				w := h.request("POST", path, fields, hx)
				requireStatus(t, w, tc.status)
				if !strings.Contains(w.Body.String(), `data-error-fields="`+tc.want+`"`) {
					t.Fatalf("missing field association %q: %s", tc.want, w.Body.String())
				}
				if hx && strings.Contains(w.Body.String(), `id="page-form"`) {
					t.Fatal("validation must not replace the mounted page editor")
				}
			}
		})
	}
}

func TestPostEditorErrorFields(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	post, err := h.blog.writePost(t.Context(), Post{Title: "Existing", Slug: "existing", Markdown: "Content", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, field, value, want string
		status                   int
	}{
		{"title", "title", " ", "title", http.StatusUnprocessableEntity},
		{"summary", "summary", strings.Repeat("s", 2001), "summary", http.StatusUnprocessableEntity},
		{"markdown", "content", " ", "content", http.StatusUnprocessableEntity},
		{"slug format", "slug", "Invalid!", "slug", http.StatusUnprocessableEntity},
		{"slug conflict", "slug", post.Slug, "slug", http.StatusConflict},
		{"tags", "tags", strings.Repeat("t", 51), "tags", http.StatusUnprocessableEntity},
		{"missing preview", "previewImage", strings.Repeat("a", 64) + ".png", "previewImage", http.StatusUnprocessableEntity},
		{"missing body image", "content", "![](/images/" + strings.Repeat("b", 64) + ".png)", "content", http.StatusUnprocessableEntity},
		{"schedule", "published", "scheduled", "publishDate publishTime", http.StatusUnprocessableEntity},
		{"publication", "published", "invalid", "published", http.StatusUnprocessableEntity},
		{"edit conflict", "version", "0", "", http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, hx := range []bool{false, true} {
				fields := url.Values{"title": {"A title"}, "content": {"Content"}, "slug": {"new-post"}}
				fields.Set(tc.field, tc.value)
				path := "/post"
				if tc.name == "edit conflict" {
					path += "/" + post.ID
				}
				w := h.request("POST", path, fields, hx)
				requireStatus(t, w, tc.status)
				if !strings.Contains(w.Body.String(), `data-error-fields="`+tc.want+`"`) {
					t.Fatalf("missing field association %q: %s", tc.want, w.Body.String())
				}
				if hx && strings.Contains(w.Body.String(), `id="post-form"`) {
					t.Fatal("validation must not replace the mounted post editor")
				}
			}
		})
	}
}

// TestEditorSaveActions checks initial labels and ordinary validation redisplays.
// Submitted choices must not be mistaken for the saved publication state.
func TestEditorSaveActions(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	publicationClock(h, now)
	button := regexp.MustCompile(`<button[^>]*data-publication-save[^>]*>([^<]+)</button>`)
	for _, kind := range []string{"post", "page"} {
		for _, state := range []string{"new", "draft", "published", "scheduled"} {
			if kind == "page" && state == "scheduled" {
				continue
			}
			t.Run(kind+"/"+state, func(t *testing.T) {
				prefix := "/post"
				if kind == "page" {
					prefix = "/pages"
				}
				path, action, version := prefix+"/create", prefix, int64(0)
				if state != "new" {
					var id string
					var err error
					if kind == "post" {
						p := Post{Title: kind + " " + state, Markdown: "Content", Draft: state == "draft"}
						if state == "scheduled" {
							p.Published = now.Add(time.Hour)
						}
						p, err = h.blog.writePost(t.Context(), p)
						id, version = p.ID, p.Version
					} else {
						p, saveErr := h.blog.writePage(t.Context(), Page{Title: kind + " " + state, ChildrenDisplay: "hidden", Published: state == "published"})
						id, version, err = p.ID, p.Version, saveErr
					}
					if err != nil {
						t.Fatal(err)
					}
					path, action = prefix+"/update/"+id, prefix+"/"+id
				}
				labels := map[string]string{"": "Save draft", "on": "Publish " + kind, "scheduled": "Schedule post"}
				initial := ""
				if state == "published" {
					initial, labels["on"], labels[""] = "on", "Save changes", "Move to drafts"
				}
				if state == "scheduled" {
					initial, labels["scheduled"], labels[""] = "scheduled", "Save schedule", "Move to drafts"
				}
				check := func(body, want string) {
					t.Helper()
					match := button.FindStringSubmatch(body)
					if len(match) != 2 || match[1] != want {
						t.Fatalf("save button = %v, want %q", match, want)
					}
				}
				for _, hx := range []bool{false, true} {
					w := h.request("GET", path, nil, hx)
					requireStatus(t, w, http.StatusOK)
					check(w.Body.String(), labels[initial])
				}
				for selection, label := range labels {
					if selection == "scheduled" && (kind == "page" || state == "published") {
						continue
					}
					fields := url.Values{"title": {""}, "content": {"Content"}, "published": {selection}, "version": {strconv.FormatInt(version, 10)}, "childrenDisplay": {"hidden"}, "publishDate": {"2030-01-02"}, "publishTime": {"9:30 AM"}}
					w := h.request("POST", action, fields, false)
					requireStatus(t, w, http.StatusUnprocessableEntity)
					check(w.Body.String(), label)
				}
			})
		}
	}
}

// TestEditorPages keeps ordinary and partial navigation on the same page-based editor.
func TestEditorPages(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, kind := range []string{"post", "page"} {
		id, _ := revisionDeleteFixture(t, h, kind)
		prefix, manager := "/post", "/post/manage"
		if kind == "page" {
			prefix, manager = "/pages", "/pages"
		}
		for _, hx := range []bool{false, true} {
			for _, path := range []string{prefix + "/create", prefix + "/update/" + id} {
				response := h.request("GET", path, nil, hx)
				requireStatus(t, response, http.StatusOK)
				body := activeMarkup(response.Body.String())
				for _, want := range []string{`class="info-page editor-panel"`, `data-draft-owner=`, `class="form-actions"`, `href="` + manager + `"`} {
					if !strings.Contains(body, want) {
						t.Fatalf("%s missing %s", path, want)
					}
				}
				if strings.Contains(body, "<dialog") || strings.Contains(body, "reader-search-form") || strings.Contains(body, "data-close-dialog") {
					t.Fatalf("%s rendered dialog controls or reader search", path)
				}
				for _, field := range []struct{ id, help, text string }{
					{kind + "-slug", "slug-help", "Custom slugs use lowercase letters"},
					{"post-content", "markdown-help", "Up to 256 KiB of Markdown"},
				} {
					control := regexp.MustCompile(`<(?:input|textarea)[^>]*id="` + field.id + `"[^>]*>`).FindString(body)
					help := regexp.MustCompile(`<small id="` + field.help + `"[^>]*>[^<]*</small>`).FindString(body)
					if !strings.Contains(control, `aria-describedby="`+field.help+`"`) || !strings.Contains(help, field.text) {
						t.Fatalf("%s missing field-specific guidance for %s", path, field.id)
					}
				}
				if strings.Contains(body, "Writing tips") {
					t.Fatalf("%s rendered the separate writing tips section", path)
				}
			}
		}
		listing := h.request("GET", manager, nil, false)
		requireStatus(t, listing, http.StatusOK)
		links := regexp.MustCompile(`<a\s[^>]*href="`+prefix+`/(create|update/[^\"]+)"[^>]*>`).FindAllString(listing.Body.String(), -1)
		if len(links) < 2 {
			t.Fatal("missing creation or editing links", kind)
		}
		for _, link := range links {
			if !strings.Contains(link, `hx-target="#article-content"`) || !strings.Contains(link, `hx-push-url="true"`) {
				t.Fatal("editor link does not use page navigation", link)
			}
		}
	}
}
