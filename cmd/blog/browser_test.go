package main

import (
	"context"
	_ "embed"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

//go:embed testdata/browser.js
var browserScript string

// TestBlogBrowser exercises the real HTMX asset, CSP, forms, and navigation.
// It is optional: HS_TEST_FIREFOX=/usr/bin/firefox go test ./cmd/blog -run TestBlogBrowser.
func TestBlogBrowser(t *testing.T) {
	path := os.Getenv("HS_TEST_FIREFOX")
	if path == "" {
		t.Skip("set HS_TEST_FIREFOX to run the browser test")
	}
	for _, width := range []string{"1440", "390"} {
		t.Run(width, func(t *testing.T) { runBlogBrowser(t, path, width) })
	}
}

func runBlogBrowser(t *testing.T, path, width string) {
	t.Helper()
	app := startTest(t, testSettings(t))
	if err := createAdmin(t.Context(), app.accounts, adminEmail, adminPassword); err != nil {
		t.Fatal(err)
	}
	handler := newHTTPServer(app).Handler
	result := make(chan string, 1)
	ready := make(chan struct{}, 1)
	var htmxPosts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test/driver.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = io.WriteString(w, browserScript)
			return
		case "/test/ready":
			select {
			case ready <- struct{}{}:
			default:
			}
			w.WriteHeader(204)
			return
		case "/test/result":
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
			if err != nil {
				t.Error(err)
				return
			}
			select {
			case result <- string(body):
			default:
			}
			w.WriteHeader(204)
			return
		}
		// Inject only into full HTML documents. The application and CSP stay unchanged.
		recorded := httptest.NewRecorder()
		if r.Method == http.MethodPost && r.Header.Get("HX-Request") == "true" {
			htmxPosts.Add(1)
		}
		handler.ServeHTTP(recorded, r)
		t.Logf("%s %s → %d", r.Method, r.URL.Path, recorded.Code)
		for key, values := range recorded.Header() {
			w.Header()[key] = values
		}
		body := recorded.Body.String()
		if strings.Contains(body, "<!doctype html>") {
			body = strings.Replace(body, "</body>", `<script src="/test/driver.js" defer></script></body>`, 1)
			w.Header().Del("Content-Length")
		}
		w.WriteHeader(recorded.Code)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--headless", "--no-remote", "--width", width, "--height", "1000", "--profile", t.TempDir(), srv.URL+"/auth/login/email")
	logFile, err := os.CreateTemp(t.TempDir(), "browser-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	defer func() { cancel(); <-exited }()
	fail := func(message string) { output, _ := os.ReadFile(logFile.Name()); t.Fatalf("%s\n%s", message, output) }
	select {
	case <-ready:
	case <-exited:
		fail("browser exited during startup")
	case <-time.After(60 * time.Second):
		fail("browser startup timed out")
	}
	select {
	case report := <-result:
		if report != "PASS" {
			fail(report)
		}
		if htmxPosts.Load() < 5 {
			fail("browser bypassed HTMX form handling")
		}
	case <-exited:
		fail("browser exited during checks")
	case <-time.After(45 * time.Second):
		fail("browser checks timed out")
	}
}
