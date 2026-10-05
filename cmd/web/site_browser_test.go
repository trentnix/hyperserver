package main

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/util"
)

// TestBrowserSiteErrors needs Firefox and network access for the integrity-checked
// HTMX version in the site layout. CI enables it with HS_TEST_FIREFOX=firefox.
func TestBrowserSiteErrors(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX to a Firefox executable to run browser tests")
	}
	runHTTPScenarioWithTimeout(t, 75*time.Second, func(h *httpHarness) {
		// Render the real layout, changing only HTMX's URL to a local copy. The
		// browser still checks the layout's integrity attribute when loading it.
		page := content.NewManagedContent(httptest.NewRequest(http.MethodGet, "/", nil), h.app.ContentManager)
		page.PartialName = "test.browser.errors"
		page.AddContent("modules/site/testdata/error-responses.html")
		w := httptest.NewRecorder()
		if err := page.Render(w, httptest.NewRequest(http.MethodGet, "/", nil)); err != nil {
			t.Fatal(err)
		}
		html := w.Body.String()
		match := regexp.MustCompile(`<script src="(https://unpkg.com/htmx.org@[^"\s]+)"\s+integrity="sha384-([^"]+)"`).FindStringSubmatch(html)
		if len(match) != 3 {
			t.Fatal("site layout must pin HTMX with a SHA-384 integrity hash")
		}
		htmx := downloadBrowserHTMX(t, match[1], match[2])
		html = strings.Replace(html, match[1], "/test/browser/htmx.js", 1)

		h.app.Web.HandleFunc("GET /test/browser", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, html)
		})
		h.app.Web.HandleFunc("GET /test/browser/htmx.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(htmx)
		})
		h.app.Web.HandleFunc("GET /test/browser/run.js", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "modules/site/testdata/error-responses.js")
		})
		for path, status := range map[string]int{"bad-request": 400, "forbidden": 403, "unavailable": 503} {
			h.app.Web.HandleFunc("GET /test/browser/"+path, func(w http.ResponseWriter, r *http.Request) {
				h.app.ContentManager.HandleError(w, r, `Public error <img id="error-injection" src=x onerror="window.errorInjected=true">`, nil, status)
			})
		}
		h.app.Web.HandleFunc("GET /test/browser/plain", func(w http.ResponseWriter, r *http.Request) {
			util.HttpError(w, r, `<img id="error-injection" src=x onerror="window.errorInjected=true">`, nil, http.StatusInternalServerError)
		})
		h.app.Web.HandleFunc("GET /test/browser/success", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, `<p id="success">Saved</p>`)
		})

		ready := make(chan struct{})
		var readyOnce sync.Once
		h.app.Web.HandleFunc("POST /test/browser/ready", func(w http.ResponseWriter, r *http.Request) {
			readyOnce.Do(func() { close(ready) })
			w.WriteHeader(http.StatusNoContent)
		})
		result := make(chan string, 1)
		h.app.Web.HandleFunc("POST /test/browser/result", func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
			if err != nil {
				http.Error(w, "invalid test result", http.StatusBadRequest)
				return
			}
			select {
			case result <- string(body):
			default:
			}
			w.WriteHeader(http.StatusNoContent)
		})
		server := httptest.NewServer(h.handler)
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		logPath := filepath.Join(t.TempDir(), "firefox.log")
		logFile, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		defer logFile.Close()
		profile := t.TempDir()
		cmd := exec.CommandContext(ctx, browser, "--headless", "--no-remote", "--profile", profile, server.URL+"/test/browser")
		cmd.Stdout, cmd.Stderr = logFile, logFile
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = 2 * time.Second
		t.Logf("Browser executable: %s\nArguments: %q\nTMPDIR: %s\nStartup limit: 30s\nCheck limit: 15s", cmd.Path, cmd.Args, os.TempDir())
		if err := cmd.Start(); err != nil {
			t.Fatalf("start browser: %v\n%s", err, browserDiagnostics(profile, logPath, h.logs.String()))
		}
		started := time.Now()
		t.Logf("Browser PID: %d", cmd.Process.Pid)
		exited := make(chan struct{})
		var exitErr error
		go func() {
			exitErr = cmd.Wait()
			close(exited)
		}()
		defer func() {
			cancel()
			<-exited
		}()

		if err := waitForBrowser(ready, result, exited, 30*time.Second, 15*time.Second); err != nil {
			// Reap the process before reading its final output and exit status.
			cancel()
			<-exited
			t.Fatalf("%v\nElapsed: %s\nBrowser exit: %v\n%s", err, time.Since(started), exitErr, browserDiagnostics(profile, logPath, h.logs.String()))
		}
	})
}

// Readiness means the page's JavaScript has started, not just that Firefox exists.
// Time spent starting the browser and loading the page does not consume check time.
func waitForBrowser(ready <-chan struct{}, result <-chan string, exited <-chan struct{}, startupTimeout, checkTimeout time.Duration) error {
	startup := time.NewTimer(startupTimeout)
	defer startup.Stop()
	select {
	case <-ready:
		startup.Stop()
	case <-exited:
		return fmt.Errorf("browser exited during startup, before JavaScript reported readiness")
	case <-startup.C:
		return fmt.Errorf("browser startup timed out after %s, before JavaScript reported readiness", startupTimeout)
	}

	checks := time.NewTimer(checkTimeout)
	defer checks.Stop()
	select {
	case report := <-result:
		if report != "PASS" {
			return fmt.Errorf("browser checks failed: %s", report)
		}
		return nil
	case <-exited:
		return fmt.Errorf("browser exited during checks, after JavaScript reported readiness")
	case <-checks.C:
		return fmt.Errorf("browser checks timed out after %s, after JavaScript reported readiness", checkTimeout)
	}
}

func browserDiagnostics(profile, logPath, requests string) string {
	entries, profileErr := os.ReadDir(profile)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	output, logErr := os.ReadFile(logPath)
	return fmt.Sprintf("Profile: %s\nProfile entries (read error: %v): %v\nBrowser output (read error: %v):\n%s\nHTTP requests:\n%s", profile, profileErr, names, logErr, output, requests)
}

func downloadBrowserHTMX(t *testing.T, url, integrity string) []byte {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("HTMX download: %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha512.Sum384(data)
	if base64.StdEncoding.EncodeToString(digest[:]) != integrity {
		t.Fatal("HTMX download does not match the site layout's integrity hash")
	}
	return data
}
