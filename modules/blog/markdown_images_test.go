package blog

import (
	"os"
	"testing"
)

func TestBrowserMarkdownImages(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check Markdown image enlargement")
	}
	for _, width := range []string{"320", "1100", "2560"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/markdown-images.js", startPath: "/post/s/image-review",
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					writing := "![Wide diagram](/test/image.svg?size=wide)\n\n" +
						"[![Linked image](/test/image.svg?size=small)](/page/image-review)\n\n" +
						"![Invalid image](javascript:example)"
					body, err := joinPostComponents(writing, componentInput{
						"callout":        `{"body":"![Callout screenshot](/test/image.svg?size=small)"}`,
						"componentOrder": `["callout"]`,
					})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := h.blog.persistPost(t.Context(), Post{
						Slug: "image-review", Title: "Markdown images", Markdown: body,
					}); err != nil {
						t.Fatal(err)
					}
					if _, err := h.blog.persistPage(t.Context(), Page{
						Slug: "image-review", Title: "Page image", Published: true, ChildrenDisplay: "hidden",
						Markdown: "![](/test/image.svg?size=tiny)",
					}); err != nil {
						t.Fatal(err)
					}
					return h
				},
			})
		})
	}
}
