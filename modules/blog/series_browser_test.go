package blog

import (
	"os"
	"strings"
	"testing"
)

func TestBrowserSeries(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check series")
	}
	for _, width := range []string{"320", "390", "1100"} {
		t.Run(width, func(t *testing.T) {
			var sourceID string
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/series.js", startPath: "/post/manage", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					post := seriesPost(t, h, "Series starting point")
					sourceID = post.ID
					for _, post := range []Post{
						{Title: "Series target second", Markdown: "Body"},
						{Title: "Series target " + strings.Repeat("with a long wrapping title ", 5), Markdown: "Body"},
						{Title: "Series target private draft", Markdown: "Body", Draft: true},
					} {
						if _, err := h.blog.persistPost(t.Context(), post); err != nil {
							t.Fatal(err)
						}
					}
					return h
				},
				prepareScript: func(t *testing.T, _ *harness, source []byte) []byte {
					return []byte(strings.ReplaceAll(string(source), "SOURCE_ID", sourceID))
				},
			})
		})
	}
}
