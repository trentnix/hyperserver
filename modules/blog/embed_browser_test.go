package blog

import (
	"os"
	"testing"
)

func TestBrowserEmbed(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check the embed editor")
	}
	for _, width := range []string{"320", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/embed.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := embedTestHarness(t)
					h.login(t)
					return h
				},
			})
		})
	}
}
