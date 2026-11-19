package blog

import (
	"os"
	"testing"
)

func TestBrowserEvent(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check event components")
	}
	for _, width := range []string{"320", "680", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/event.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					var err error
					h.blog.location, err = loadTimeZone("America/Denver")
					if err != nil {
						t.Fatal(err)
					}
					h.login(t)
					return h
				},
			})
		})
	}
}
