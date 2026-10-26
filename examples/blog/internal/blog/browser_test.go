package blog

import (
	"bytes"
	"context"
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

// testBrowser serves the real blog with isolated data and browser checks.
func testBrowser(t *testing.T, browser, width, mode string) {
	t.Helper()
	h := newHarness(t)
	h.register(t)
	h.verify(t)
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
	case "navigation":
		scriptFile = "testdata/navigation.js"
	case "editor-history":
		scriptFile = "testdata/editor-history.js"
	case "projects":
		scriptFile = "testdata/projects.js"
	case "pages":
		scriptFile = "testdata/pages.js"
	case "profile":
		scriptFile = "testdata/profile.js"
	case "auth-styles":
		scriptFile = "testdata/auth-styles.js"
	}
	script, err := os.ReadFile(scriptFile)
	if err != nil {
		t.Fatal(err)
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
	pagePath := "/auth/login/email"
	if mode == "layout" {
		pagePath += "?test-layout=1&amp;test-width=" + width
	} else if mode == "navigation" {
		pagePath = "/post/s/first"
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			fmt.Fprintf(w, `<!DOCTYPE html><iframe src="%s" style="border:0;width:%spx;height:1000px"></iframe>`, pagePath, width)
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
			w.Write(script)
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
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/html") {
			body = bytes.Replace(body, []byte("</head>"), []byte(`<script src="/test/browser.js" defer></script></head>`), 1)
			w.Header().Del("Content-Length")
		}
		w.WriteHeader(recorder.Code)
		w.Write(body)
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
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
	defer func() { cancel(); command.Wait() }()
	select {
	case report := <-result:
		if report != "PASS" {
			t.Fatal(report)
		}
	case <-ctx.Done():
		data, _ := os.ReadFile(log.Name())
		t.Fatalf("browser did not finish: %s", data)
	}
}
