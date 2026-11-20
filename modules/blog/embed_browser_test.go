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

func TestBrowserComponentPlacement(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check component placement")
	}
	for _, width := range []string{"240", "320", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/component-placement.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := embedTestHarness(t)
					h.login(t)
					return h
				},
			})
		})
	}
}

func TestBrowserComponentTransactions(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check component editing and history")
	}
	testBrowser(t, browser, "320", browserScenario{
		scriptFile: "testdata/component-transactions.js", startPath: "/post/create", authenticated: true,
		setup: func(t *testing.T) *harness {
			h := embedTestHarness(t)
			h.login(t)
			return h
		},
	})
}

func TestBrowserComponentInstances(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check repeated components")
	}
	for _, width := range []string{"320", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/component-instances.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := embedTestHarness(t)
					h.login(t)
					return h
				},
			})
		})
	}
}
