package blog

import (
	"os"
	"testing"
)

func TestBrowserComparison(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check image comparisons")
	}
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/comparison.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					mediaUpload(t, h.blog, "Before comparison")
					mediaUpload(t, h.blog, "After comparison")
					return h
				},
			})
		})
	}
}
