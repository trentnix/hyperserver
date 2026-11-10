package blog

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/messages"
)

// A nil setup uses a registered, verified administrator. Optional hooks prepare
// scenario data and endpoints before the server starts. The runner owns Firefox,
// reporting, framing, and cleanup.
type browserScenario struct {
	scriptFile, startPath        string
	setup                        func(*testing.T) *harness
	prepareScript                func(*testing.T, *harness, []byte) []byte
	routes                       func(*harness) map[string]http.HandlerFunc
	height                       int
	authenticated, returningUser bool
	delayAsset                   func(*http.Request) bool
	earlyScript, allowFrames     bool
}

func browserFontAsset(r *http.Request) bool {
	return strings.HasSuffix(r.URL.Path, ".woff2")
}

// testBrowser serves the real blog with isolated data and browser checks.
func testBrowser(t *testing.T, browser, width string, scenario browserScenario) {
	t.Helper()
	setup := scenario.setup
	if setup == nil {
		setup = registeredBrowserHarness
	}
	h := setup(t)
	script, err := os.ReadFile(scenario.scriptFile)
	if err != nil {
		t.Fatal(err)
	}
	if scenario.prepareScript != nil {
		script = scenario.prepareScript(t, h, script)
	}
	var routes map[string]http.HandlerFunc
	if scenario.routes != nil {
		routes = scenario.routes(h)
	}
	height := scenario.height
	if height == 0 {
		height = 1000
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
	result := make(chan string, 1)
	ready := make(chan struct{})
	var readyOnce sync.Once
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if scenario.delayAsset != nil && scenario.delayAsset(r) {
			// Leave time for the initial layout to paint before enhancement and fonts arrive.
			select {
			case <-r.Context().Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
		if handler := routes[r.URL.Path]; handler != nil {
			handler(w, r)
			return
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
			if scenario.authenticated {
				for _, cookie := range h.cookies {
					http.SetCookie(w, cookie)
				}
			}
			if scenario.returningUser {
				http.SetCookie(w, &http.Cookie{Name: returningUserCookie, Value: "1", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
			}
			// Firefox limits ordinary windows to 500px. An iframe gives each
			// flow an exact viewport, including narrow mobile sizes.
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html><iframe src="%s" style="border:0;width:%spx;height:%dpx"></iframe>`, html.EscapeString(scenario.startPath), width, height)
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
		if scenario.allowFrames {
			// A second editor frame exercises separate-tab storage ownership.
			w.Header().Set("Content-Security-Policy", w.Header().Get("Content-Security-Policy")+"; frame-src 'self'")
		}
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/html") {
			scriptTag := `<script src="/test/browser.js" defer></script></head>`
			if scenario.earlyScript {
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
