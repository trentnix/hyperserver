package blog

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// activeMarkup omits inert template contents when checking the rendered interface.
func activeMarkup(markup string) string {
	return regexp.MustCompile(`(?s)<template\b[^>]*>.*?</template>`).ReplaceAllString(markup, "")
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
