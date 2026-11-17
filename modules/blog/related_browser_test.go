package blog

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestBrowserRelatedPosts(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check related posts")
	}
	for _, width := range []string{"320", "375", "390", "414", "520", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/related.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					for _, post := range []Post{
						{Title: "A long target " + strings.Repeat("with wrapping words ", 8), Markdown: "Body"},
						{Title: "Second target", Markdown: "Body"},
						{Title: "Private draft target", Markdown: "Body", Draft: true},
					} {
						if _, err := h.blog.persistPost(t.Context(), post); err != nil {
							t.Fatal(err)
						}
					}
					for i := range 9 {
						if _, err := h.blog.persistPost(t.Context(), Post{Title: "Capacity target " + strconv.Itoa(i), Markdown: "Body"}); err != nil {
							t.Fatal(err)
						}
					}
					return h
				},
			})
		})
	}
}
