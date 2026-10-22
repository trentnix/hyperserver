package blog

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
		t.Run(width, func(t *testing.T) { testBrowserEditor(t, browser, width) })
	}
}

func testBrowserEditor(t *testing.T, browser, width string) {
	t.Helper()
	h := newHarness(t)
	h.register(t)
	h.verify(t)
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
	script, err := os.ReadFile("testdata/browser.js")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan string, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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
	command := exec.CommandContext(ctx, browser, "--headless", "--no-remote", "--profile", profile, "--window-size", width+",1000", s.URL+"/auth/login/email")
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
