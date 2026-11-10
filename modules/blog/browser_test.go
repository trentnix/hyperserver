package blog

import (
	"net/http"
	"os"
	"testing"
)

// TestBrowserEditor exercises the real HTMX and EasyMDE bundles against isolated
// account and post storage. Enable with HS_TEST_FIREFOX=firefox.
func TestBrowserEditor(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to exercise the editor in a browser")
	}
	for _, width := range []string{"390", "900", "1100", "1440", "2560"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/browser.js",
				startPath:  "/login",
			})
		})
	}
}

// TestBrowserAccessibility checks navigation focus, editor keyboard behavior,
// control labels, and dialog focus at mobile, medium, and ultrawide widths.
func TestBrowserAccessibility(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check keyboard accessibility")
	}
	for _, width := range []string{"320", "768", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/accessibility.js",
				startPath:  "/login",
			})
		})
	}
}

// TestBrowserSetup follows registration, verification, and login into the profile editor.
func TestBrowserSetup(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check first-time setup")
	}
	for _, width := range []string{"320", "1100", "2560"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/setup.js",
				startPath:  "/",
				setup:      setupBrowserHarness,
				routes:     setupBrowserRoutes,
			})
		})
	}
}

// TestBrowserLayout checks the layout breakpoint, ultrawide centering, and minimum
// page width without repeating the editor and notification flows at every width.
func TestBrowserLayout(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check responsive layout")
	}
	for _, width := range []string{"280", "320", "767", "768", "1679", "1680", "1920", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/browser.js",
				startPath:  "/login?test-layout=1&test-width=" + width,
			})
		})
	}
}

// TestBrowserFonts checks font assets and layout when optional display fonts arrive late.
func TestBrowserFonts(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check delayed font loading")
	}
	testBrowser(t, browser, "1100", browserScenario{
		scriptFile: "testdata/browser.js",
		startPath:  "/login?test-layout=1&test-width=1100",
		delayAsset: browserFontAsset,
	})
}

// TestBrowserRefresh checks first-paint stability while scripts and fonts load slowly.
func TestBrowserRefresh(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check first-paint stability")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile:  "testdata/refresh.js",
				startPath:   "/login",
				delayAsset:  func(r *http.Request) bool { return r.URL.Path == "/assets/js/blog.js" || browserFontAsset(r) },
				earlyScript: true,
			})
		})
	}
}

// TestBrowserSearch checks reader search, partial navigation, and responsive layout.
func TestBrowserSearch(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check reader search")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/search.js",
				startPath:  "/posts",
				setup:      searchBrowserHarness,
			})
		})
	}
}

// TestBrowserArchivePagination checks archive links, partial swaps, and browser history.
func TestBrowserArchivePagination(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check archive pagination")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile:    "testdata/archive-pagination.js",
				startPath:     "/posts",
				setup:         archiveBrowserHarness,
				prepareScript: archiveBrowserScript,
			})
		})
	}
}

// TestBrowserNavigation checks article swaps and history without replacing the sidebar.
func TestBrowserNavigation(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check article navigation")
	}
	for _, width := range []string{"320", "390", "767", "768", "1100", "1679", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile:    "testdata/navigation.js",
				startPath:     "/post/s/first",
				setup:         navigationBrowserHarness,
				returningUser: true,
			})
		})
	}
}

// TestBrowserEditorHistory checks editor initialization after history restoration.
func TestBrowserEditorHistory(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check editor history restoration")
	}
	testBrowser(t, browser, "1100", browserScenario{
		scriptFile: "testdata/editor-history.js",
		startPath:  "/login",
		setup:      editorHistoryBrowserHarness,
	})
}

// TestBrowserManagementRefresh checks lists restored from the browser's page cache.
func TestBrowserManagementRefresh(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check restored management lists")
	}
	testBrowser(t, browser, "1100", browserScenario{
		scriptFile: "testdata/management-refresh.js",
		startPath:  "/login",
	})
}

// TestBrowserRevisions follows full-page history, loading, recovery, and explicit
// saving with the real editor. History must never open inside an editor dialog.
func TestBrowserRevisions(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check revision history")
	}
	for _, width := range []string{"390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile:    "testdata/revisions.js",
				startPath:     "/login",
				prepareScript: revisionBrowserScript(2),
			})
		})
	}
}

// TestBrowserRevisionRestore applies saved writing directly and selects the new history entry.
func TestBrowserRevisionRestore(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check direct restoration")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile:    "testdata/revision-restore.js",
				startPath:     "/login",
				prepareScript: revisionBrowserScript(2),
			})
		})
	}
}

// TestBrowserRevisionNavigation keeps long history lists and scroll positions intact during selection.
func TestBrowserRevisionNavigation(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check in-place history selection")
	}
	for _, width := range []string{"390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile:    "testdata/revision-navigation.js",
				startPath:     "/login",
				prepareScript: revisionBrowserScript(40),
			})
		})
	}
}

// TestBrowserProjects checks project editing, preview uploads, and responsive cards.
func TestBrowserProjects(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check project editing")
	}
	for _, width := range []string{"390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/projects.js",
				startPath:  "/login",
			})
		})
	}
}

// TestBrowserPages checks standalone content management with the shared editor.
func TestBrowserPages(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check page editing")
	}
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/pages.js",
				startPath:  "/login",
				setup:      pagesBrowserHarness,
			})
		})
	}
}

// TestBrowserPostManagement checks action alignment beside wrapping post titles.
func TestBrowserPostManagement(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check post management layout")
	}
	for _, width := range []string{"320", "767", "768", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/post-management.js",
				startPath:  "/login",
				setup:      postManagementBrowserHarness,
			})
		})
	}
}

// TestBrowserPostPublication exercises publication choices through creation and edits.
func TestBrowserPostPublication(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check post publication")
	}
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/post-publication.js",
				startPath:  "/login",
			})
		})
	}
}

// TestBrowserScheduling checks UTC scheduling, editor visibility, and cancellation.
func TestBrowserScheduling(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check scheduled publication")
	}
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/scheduling.js",
				startPath:  "/login",
				// Expose clipped time options at the editor's scroll limit.
				height: 600,
			})
		})
	}
}

// TestBrowserSlugWarnings checks publication-aware confirmation without losing edits.
func TestBrowserSlugWarnings(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check slug warnings")
	}
	for _, width := range []string{"390", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/slug-warnings.js",
				startPath:  "/login",
			})
		})
	}
}

// TestBrowserConfirmations exercises real styled confirmations and their default actions.
func TestBrowserConfirmations(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check editor confirmations")
	}
	for _, width := range []string{"320", "390", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/confirmations.js",
				startPath:  "/login",
			})
		})
	}
}

// TestBrowserDraftRecovery checks unsaved-edit warnings and browser-local recovery.
func TestBrowserDraftRecovery(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check draft recovery")
	}
	for _, width := range []string{"390", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile:  "testdata/draft-recovery.js",
				startPath:   "/login",
				allowFrames: true,
			})
		})
	}
}

// TestBrowserProfile checks profile edits across the header, favicon, and background.
func TestBrowserProfile(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check profile editing")
	}
	for _, width := range []string{"390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/profile.js",
				startPath:  "/login",
			})
		})
	}
}

// TestBrowserProfileHistory checks responsive previews, stable selection, and explicit restoration.
func TestBrowserProfileHistory(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check profile history")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/profile-history.js",
				startPath:  "/login",
				setup:      profileHistoryBrowserHarness,
			})
		})
	}
}

// TestBrowserAuthStyles compares authentication panels in full-page and HTMX responses.
func TestBrowserAuthStyles(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check authentication styles")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile:    "testdata/auth-styles.js",
				startPath:     "/login",
				prepareScript: resetBrowserScript,
			})
		})
	}
}

// TestBrowserFormErrors checks shared validation and operation feedback at each layout.
func TestBrowserFormErrors(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check shared form errors")
	}
	for _, width := range []string{"390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile:    "testdata/form-errors.js",
				startPath:     "/login",
				setup:         formErrorsBrowserHarness,
				prepareScript: resetBrowserScript,
			})
		})
	}
}

// TestBrowserAuthChoices checks login and registration controls at each layout.
func TestBrowserAuthChoices(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check provider choices")
	}
	for _, scenario := range []struct {
		name, startPath      string
		email, setupComplete bool
	}{
		{"auth-choices-both", "/login?email=true", true, true},
		{"auth-choices-google", "/login?email=false", false, true},
		{"auth-choices-register-both", "/register?email=true", true, false},
		{"auth-choices-register-google", "/register?email=false", false, false},
	} {
		for _, width := range []string{"320", "390", "500", "767", "1100", "3440"} {
			t.Run(scenario.name+"/"+width, func(t *testing.T) {
				testBrowser(t, browser, width, browserScenario{
					scriptFile: "testdata/auth-choices.js",
					startPath:  scenario.startPath,
					setup:      authChoicesBrowserHarness(scenario.email, scenario.setupComplete),
				})
			})
		}
	}
}

// TestBrowserMediaCleanup exercises review, cancellation, deletion, and responsive layout.
func TestBrowserMediaCleanup(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check media cleanup")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/media-cleanup.js",
				startPath:  "/login",
				setup:      mediaCleanupBrowserHarness,
			})
		})
	}
}

// TestBrowserMediaLibrary checks reuse without losing unsaved content at each layout size.
func TestBrowserMediaLibrary(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check media reuse")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/media-library.js",
				startPath:  "/login",
				setup:      mediaLibraryBrowserHarness,
			})
		})
	}
}

// TestBrowserAdminNavigation keeps the page shell during management navigation.
func TestBrowserAdminNavigation(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check management navigation")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/admin-navigation.js",
				startPath:  "/login",
				setup:      adminNavigationBrowserHarness,
			})
		})
	}
}

// TestBrowserAccount checks the Account layout and inline password enrollment.
func TestBrowserAccount(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check Account")
	}
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile:    "testdata/account.js",
				startPath:     "/auth/account",
				setup:         accountBrowserHarness,
				routes:        accountBrowserRoutes,
				authenticated: true,
			})
		})
	}
}

// TestBrowserTags checks suggestions and browser recovery in both writing forms.
func TestBrowserTags(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check tag editing")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/tags.js",
				startPath:  "/login",
				setup:      tagsBrowserHarness,
			})
		})
	}
}

// TestBrowserAlbum checks editing and anonymous viewing at each layout size.
func TestBrowserAlbum(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check photo albums")
	}
	for _, width := range []string{"320", "390", "600", "768", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/album.js",
				startPath:  "/login",
				setup:      albumBrowserHarness,
			})
		})
	}
}

func TestBrowserMediaLandscape(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check landscape image selection")
	}
	for _, width := range []string{"600", "740", "1024"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/media-landscape.js",
				startPath:  "/login",
				setup:      mediaLibraryBrowserHarness,
				height:     map[string]int{"600": 320, "740": 360, "1024": 500}[width],
			})
		})
	}
}
