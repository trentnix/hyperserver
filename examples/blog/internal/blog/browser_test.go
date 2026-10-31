package blog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/messages"
)

// TestBrowserEditor exercises the real HTMX and EasyMDE bundles against isolated
// account and post storage. Enable with HS_TEST_FIREFOX=firefox.
func TestBrowserEditor(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to exercise the editor in a browser")
	}
	for _, width := range []string{"390", "900", "1100", "1440", "2560"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "") })
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
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "layout") })
	}
}

// TestBrowserRefresh checks first-paint stability while scripts and fonts load slowly.
func TestBrowserRefresh(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check first-paint stability")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "refresh") })
	}
}

// TestBrowserArchivePagination checks archive links, partial swaps, and browser history.
func TestBrowserArchivePagination(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check archive pagination")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "archive") })
	}
}

// TestBrowserNavigation checks article swaps and history without replacing the sidebar.
func TestBrowserNavigation(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check article navigation")
	}
	for _, width := range []string{"320", "390", "767", "768", "1100", "1679", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "navigation") })
	}
}

// TestBrowserEditorHistory checks editor initialization after history restoration.
func TestBrowserEditorHistory(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check editor history restoration")
	}
	testBrowser(t, browser, "1100", "editor-history")
}

// TestBrowserRevisions follows full-page history, loading, recovery, and explicit
// saving with the real editor. History must never open inside an editor dialog.
func TestBrowserRevisions(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check revision history")
	}
	for _, width := range []string{"390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "revisions") })
	}
}

// TestBrowserRevisionNavigation keeps long history lists and scroll positions intact during selection.
func TestBrowserRevisionNavigation(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check in-place history selection")
	}
	for _, width := range []string{"390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "revision-navigation") })
	}
}

// TestBrowserProjects checks project editing, preview uploads, and responsive cards.
func TestBrowserProjects(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check project editing")
	}
	for _, width := range []string{"390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "projects") })
	}
}

// TestBrowserPages checks standalone content management with the shared editor.
func TestBrowserPages(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check page editing")
	}
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "pages") })
	}
}

// TestBrowserPostManagement checks action alignment beside wrapping post titles.
func TestBrowserPostManagement(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check post management layout")
	}
	for _, width := range []string{"320", "767", "768", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "post-management") })
	}
}

// TestBrowserPostPublication exercises publication choices through creation and edits.
func TestBrowserPostPublication(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check post publication")
	}
	for _, width := range []string{"390", "1100"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "post-publication") })
	}
}

// TestBrowserScheduling checks UTC scheduling, editor visibility, and cancellation.
func TestBrowserScheduling(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check scheduled publication")
	}
	for _, width := range []string{"320", "390", "1100"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "scheduling") })
	}
}

// TestBrowserSlugWarnings checks publication-aware confirmation without losing edits.
func TestBrowserSlugWarnings(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check slug warnings")
	}
	for _, width := range []string{"390", "1100"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "slug-warnings") })
	}
}

// TestBrowserConfirmations exercises real styled confirmations and their default actions.
func TestBrowserConfirmations(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check editor confirmations")
	}
	for _, width := range []string{"320", "390", "1100"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "confirmations") })
	}
}

// TestBrowserDraftRecovery checks unsaved-edit warnings and browser-local recovery.
func TestBrowserDraftRecovery(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check draft recovery")
	}
	for _, width := range []string{"390", "1100"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "draft-recovery") })
	}
}

// TestBrowserProfile checks profile edits across the header, favicon, and background.
func TestBrowserProfile(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check profile editing")
	}
	for _, width := range []string{"390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "profile") })
	}
}

// TestBrowserAuthStyles compares authentication panels in full-page and HTMX responses.
func TestBrowserAuthStyles(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check authentication styles")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "auth-styles") })
	}
}

// TestBrowserMediaCleanup exercises review, cancellation, deletion, and responsive layout.
func TestBrowserMediaCleanup(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check media cleanup")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "media-cleanup") })
	}
}

// TestBrowserMediaLibrary checks reuse without losing unsaved content at each layout size.
func TestBrowserMediaLibrary(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check media reuse")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "media-library") })
	}
}

// TestBrowserAdminNavigation keeps the page shell during management navigation.
func TestBrowserAdminNavigation(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check management navigation")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "admin-navigation") })
	}
}

// testBrowser serves the real blog with isolated data and browser checks.
func testBrowser(t *testing.T, browser, width, mode string) {
	t.Helper()
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	if mode == "archive" {
		seedArchive(t, h, 2*archivePageSize+1)
	}
	if mode == "media-library" {
		for i := range mediaPageSize + 1 {
			mediaUpload(t, h.blog, fmt.Sprintf("library image %d", i))
		}
	}
	if mode == "media-cleanup" {
		mediaUpload(t, h.blog, "unused browser image")
		historical := mediaUpload(t, h.blog, "historical browser image")
		post, err := h.blog.posts.Save(t.Context(), Post{Slug: "historical-image", Title: "Historical image", Markdown: "![Old](/images/" + historical.ID + ")", PreviewImage: historical.ID, Draft: true})
		if err != nil {
			t.Fatal(err)
		}
		post.Markdown, post.PreviewImage = "No images", ""
		if _, err := h.blog.posts.Save(t.Context(), post); err != nil {
			t.Fatal(err)
		}
		used := mediaUpload(t, h.blog, "saved browser image")
		if _, err := h.blog.posts.Save(t.Context(), Post{Slug: "image", Title: "Saved image", PreviewImage: used.ID, Draft: true}); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "admin-navigation" {
		seedContentSearch(t, h)
		if _, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "visible-child", Title: "Visible child", ParentID: h.projectsID, Published: true}); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "post-management" {
		for _, draft := range []bool{true, false} {
			for i, title := range []string{"Short title", strings.Repeat("A longer post title with several words ", 6), strings.Repeat("W", 240)} {
				if _, err := h.blog.posts.Save(t.Context(), Post{Slug: fmt.Sprintf("layout-%t-%d", draft, i), Title: title, Markdown: "Body", Draft: draft}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if mode == "pages" {
		child, err := h.blog.pageStore.SavePage(t.Context(), Page{ParentID: h.projectsID, Slug: "sample-project", Title: "A sample project with a longer title", Published: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.blog.pageStore.SavePage(t.Context(), Page{ParentID: child.ID, Slug: "sample-guide", Title: "A draft guide"}); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "navigation" {
		h.login(t)
		if _, err := h.blog.pageStore.SavePage(t.Context(), Page{Slug: "another-page", Title: "Another page with a longer title", Published: true}); err != nil {
			t.Fatal(err)
		}
		for i := range 6 {
			title := "Sample project"
			if i == 0 {
				title = strings.Repeat("A long project title ", 8) + `"notes" <guide>`
			} else if i == 1 {
				title = "Short"
			}
			_, err := h.blog.pageStore.SavePage(t.Context(), Page{ParentID: h.projectsID,
				Slug: fmt.Sprintf("project-%d", i), Title: title, Summary: "A project card for navigation layout checks.", Published: true,
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		project, err := h.blog.pageStore.GetPageBySlug(t.Context(), "project-0", true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.blog.pageStore.SavePage(t.Context(), Page{ParentID: project.ID, Slug: "project-guide", Title: "Project guide", Published: true}); err != nil {
			t.Fatal(err)
		}
		for _, slug := range []string{"first", "second"} {
			body := "Body of " + slug
			if slug == "first" {
				body = strings.Repeat(body+"\n\n", 100)
			}
			response := h.request(http.MethodPost, "/post", url.Values{
				"title": {slug + " <post>"}, "slug": {slug}, "content": {body},
			}, false)
			requireStatus(t, response, http.StatusSeeOther)
			h.publish(t, slug)
		}
	}
	loadNotifications := h.app.ContentManager.LoadNotifications
	h.app.ContentManager.LoadNotifications = func(r *http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
		if r.URL.Query().Get("test-notifications") == "1" {
			return []messages.Notification{
				messages.NewNotification("Saved successfully", messages.NotificationTypeSuccess),
				messages.NewNotification("Please correct this error", messages.NotificationTypeError),
			}, nil, nil
		}
		return loadNotifications(r)
	}
	scriptFile := "testdata/browser.js"
	switch mode {
	case "archive":
		scriptFile = "testdata/archive-pagination.js"
	case "refresh":
		scriptFile = "testdata/refresh.js"
	case "admin-navigation":
		scriptFile = "testdata/admin-navigation.js"
	case "media-library":
		scriptFile = "testdata/media-library.js"
	case "media-cleanup":
		scriptFile = "testdata/media-cleanup.js"
	case "navigation":
		scriptFile = "testdata/navigation.js"
	case "editor-history":
		scriptFile = "testdata/editor-history.js"
	case "revisions":
		scriptFile = "testdata/revisions.js"
	case "revision-navigation":
		scriptFile = "testdata/revision-navigation.js"
	case "projects":
		scriptFile = "testdata/projects.js"
	case "pages":
		scriptFile = "testdata/pages.js"
	case "profile":
		scriptFile = "testdata/profile.js"
	case "auth-styles":
		scriptFile = "testdata/auth-styles.js"
	case "draft-recovery":
		scriptFile = "testdata/draft-recovery.js"
	case "confirmations":
		scriptFile = "testdata/confirmations.js"
	case "post-publication":
		scriptFile = "testdata/post-publication.js"
	case "scheduling":
		scriptFile = "testdata/scheduling.js"
	case "post-management":
		scriptFile = "testdata/post-management.js"
	case "slug-warnings":
		scriptFile = "testdata/slug-warnings.js"
	}
	script, err := os.ReadFile(scriptFile)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "revisions" || mode == "revision-navigation" {
		var targets []map[string]any
		store := h.blog.posts.(*sqliteRepository)
		for _, kind := range []string{"post", "page"} {
			original := Revision{Title: "Original title", Summary: "Original summary", Markdown: "# Original body"}
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, original)
			if err != nil {
				t.Fatal(err)
			}
			list, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil || len(list) != 1 {
				t.Fatal(list, err)
			}
			_, version, err = saveRevisionContent(t.Context(), store, kind, id, version, Revision{Title: "Current title", Summary: "Current summary\r\nSecond line", Markdown: "Current body\r\nSecond line"})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "revision-navigation" {
				for i := 3; i <= 40; i++ {
					_, version, err = saveRevisionContent(t.Context(), store, kind, id, version, Revision{Title: fmt.Sprintf("Version %d", i), Markdown: "Saved writing"})
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			prefix := "/post"
			if kind == "page" {
				prefix = "/pages"
			}
			view, manager := "/post/s/history", "/post/manage"
			if kind == "page" {
				view, manager = "/page/history", "/pages"
			}
			targets = append(targets, map[string]any{"editor": prefix + "/update/" + id, "history": "/history/" + kind + "/" + id, "revision": list[0].ID, "view": view, "manager": manager})
		}
		data, err := json.Marshal(targets)
		if err != nil {
			t.Fatal(err)
		}
		script = bytes.ReplaceAll(script, []byte("__REVISION_TARGETS__"), data)
	}
	if mode == "auth-styles" {
		requireStatus(t, h.request("POST", "/auth/reset/request/email", url.Values{"email": {"trentnix@gmail.com"}}, false), http.StatusOK)
		if len(h.mail.messages) != 2 {
			t.Fatal("expected one reset email after registration")
		}
		link := regexp.MustCompile(`https://blog.example/auth/reset/email\?[^"< ]+`).FindString(h.mail.messages[1].Body)
		resetURL, err := url.Parse(html.UnescapeString(link))
		if err != nil || resetURL.Query().Get("token") == "" {
			t.Fatal("missing reset link", err)
		}
		script = bytes.ReplaceAll(script, []byte("__RESET_URL__"), []byte(fmt.Sprintf("%q", resetURL.RequestURI())))
	}
	result := make(chan string, 1)
	ready := make(chan struct{})
	var readyOnce sync.Once
	pagePath := "/auth/login/email"
	if mode == "layout" {
		pagePath += "?test-layout=1&amp;test-width=" + width
	} else if mode == "navigation" {
		pagePath = "/post/s/first"
	} else if mode == "archive" {
		pagePath = "/posts"
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "refresh" && (r.URL.Path == "/assets/js/blog.js" || strings.HasSuffix(r.URL.Path, ".woff2")) {
			// Leave time for the initial layout to paint before enhancement and fonts arrive.
			select {
			case <-r.Context().Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
		switch r.URL.Path {
		case "/test/image.svg":
			sizes := map[string][2]int{"large": {3200, 1800}, "small": {160, 100}, "tall": {360, 1800}, "wide": {2400, 320}, "square": {900, 900}, "tiny": {48, 48}}
			size, ok := sizes[r.URL.Query().Get("size")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/svg+xml")
			fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d"><rect width="100%%" height="100%%" fill="#4477aa"/></svg>`, size[0], size[1])
			return
		case "/test/viewport":
			// Firefox limits ordinary windows to 500px. An iframe gives each
			// flow an exact viewport, including narrow mobile sizes.
			w.Header().Set("Content-Type", "text/html")
			height := 1000
			if mode == "scheduling" {
				height = 600 // A short viewport exposes clipped time options at the editor's scroll limit.
			}
			fmt.Fprintf(w, `<!DOCTYPE html><iframe src="%s" style="border:0;width:%spx;height:%dpx"></iframe>`, pagePath, width, height)
			return
		case "/test/plain-error":
			http.Error(w, `<img src=x onerror="window.errorInjected=true"> Please try again.`, http.StatusTooManyRequests)
			return
		case "/test/json-error":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":"private detail"}`)
			return
		case "/test/disconnect":
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close()
			return
		case "/test/timeout":
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
				w.WriteHeader(http.StatusNoContent)
			}
			return
		case "/test/browser.js":
			w.Header().Set("Content-Type", "text/javascript")
			io.WriteString(w, `document.addEventListener('DOMContentLoaded', () => fetch('/test/ready', {method: 'POST'}), {once: true});`+"\n")
			w.Write(script)
			return
		case "/test/ready":
			readyOnce.Do(func() { close(ready) })
			w.WriteHeader(http.StatusNoContent)
			return
		case "/test/result":
			data, err := io.ReadAll(io.LimitReader(r.Body, 8192))
			if err != nil {
				t.Error(err)
			}
			select {
			case result <- string(data):
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		recorder := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(recorder, r)
		if !strings.HasPrefix(r.URL.Path, "/assets/") {
			t.Logf("%s %s: %d", r.Method, r.URL.Path, recorder.Code)
		}
		for name, values := range recorder.Header() {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		body := recorder.Body.Bytes()
		// Allow only this test server to frame the page at the requested size.
		// Production responses still prohibit framing.
		w.Header().Set("Content-Security-Policy", strings.ReplaceAll(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'", "frame-ancestors 'self'"))
		if mode == "draft-recovery" {
			// A second editor frame exercises separate-tab storage ownership.
			w.Header().Set("Content-Security-Policy", w.Header().Get("Content-Security-Policy")+"; frame-src 'self'")
		}
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/html") {
			scriptTag := `<script src="/test/browser.js" defer></script></head>`
			if mode == "refresh" {
				scriptTag = `<script src="/test/browser.js"></script></head>`
			}
			body = bytes.Replace(body, []byte("</head>"), []byte(scriptTag), 1)
			w.Header().Del("Content-Length")
		}
		w.WriteHeader(recorder.Code)
		w.Write(body)
	}))
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	log, err := os.CreateTemp(t.TempDir(), "firefox-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	profile := t.TempDir()
	// Exercise reduced-motion dismissal in the mobile run, with normal fades elsewhere.
	if width == "390" {
		if err := os.WriteFile(profile+"/user.js", []byte(`user_pref("ui.prefersReducedMotion", 1);`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pageURL := s.URL + "/test/viewport"
	command := exec.CommandContext(ctx, browser, "--headless", "--no-remote", "--profile", profile, "--width", width, "--height", "1000", pageURL)
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		command.Wait()
		close(exited)
	}()
	defer func() { cancel(); <-exited }()
	readyAfter, err := waitForBlogBrowser(ctx, ready, result, exited)
	if readyAfter > 0 {
		t.Logf("Browser JavaScript ready after %s", readyAfter)
	}
	if err != nil {
		data, _ := os.ReadFile(log.Name())
		t.Fatalf("%v\nFirefox output:\n%s", err, data)
	}
}
