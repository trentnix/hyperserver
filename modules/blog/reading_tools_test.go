package blog

import (
	"os"
	"strings"
	"testing"
)

const readingExample = "## Café &amp; setup\n\n```go\nif ready {\n\tstart()\n}\n```\n\n" +
	"    curl --silent --show-error --fail-with-body --request GET --header 'Accept: text/html' --output /tmp/reading-tools-preview.html http://127.0.0.1:8086/post/s/reading-tools-review\n\n" +
	"## Café &amp; setup\n\n> ## Quoted text\n"

func TestMarkdownHeadingLinks(t *testing.T) {
	rendered, err := markdown(readingExample)
	if err != nil {
		t.Fatal(err)
	}
	for _, markup := range []string{
		`<h2 id="section-café-setup">Café &amp; setup</h2>`,
		`<h2 id="section-café-setup-2">Café &amp; setup</h2>`,
		`<h2>Quoted text</h2>`,
	} {
		if !strings.Contains(string(rendered), markup) {
			t.Fatalf("expected %s in %s", markup, rendered)
		}
	}
}

func TestBrowserReadingTools(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check the reading tools")
	}
	for _, width := range []string{"320", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/reading-tools.js", startPath: "/", returningUser: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					if _, err := h.blog.persistPost(t.Context(), Post{
						Slug: "reading-tools", Title: "Reading tools", Markdown: readingExample,
					}); err != nil {
						t.Fatal(err)
					}
					if _, err := h.blog.persistPage(t.Context(), Page{
						Slug: "reading-tools", Title: "Reading tools on a page", Markdown: readingExample,
						Published: true, ChildrenDisplay: "hidden",
					}); err != nil {
						t.Fatal(err)
					}
					return h
				},
			})
		})
	}
}
