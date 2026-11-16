package blog

import (
	"os"
	"testing"
)

func TestBrowserLink(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check link previews")
	}
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/link.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					installLinkFixture(t, h.blog)
					return h
				},
			})
		})
	}
}
