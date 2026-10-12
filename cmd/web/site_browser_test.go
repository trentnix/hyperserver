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
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/services/session"
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
		r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.app.SessionManager)
		page := content.NewManagedContent(r, h.app.ContentManager)
		page.PartialName = "test.browser.errors"
		page.SystemMessages = []messages.SystemMessage{messages.NewSystemMessage(`</script><script>window.systemMessageInjected=true</script>`, messages.SystemMessageTypeDebug)}
		file := testTemplateFile(t, "modules/site/testdata/error-responses.html")
		page.AddContent(file.FS, file.Path)
		w := httptest.NewRecorder()
		if err := page.Render(w, r); err != nil {
			t.Fatal(err)
		}
		html, htmx := localBrowserHTMX(t, w.Body.String())

		h.app.Web.HandleFunc("GET /test/browser", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, html)
		})
		h.app.Web.HandleFunc("GET /test/browser/htmx.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(htmx)
		})
		h.app.Web.HandleFunc("GET /test/browser/run.js", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join(h.app.Config.App.WorkingDirectory, "modules/site/testdata/error-responses.js"))
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
			io.WriteString(w, `<p id="success">Saved</p><script src="/test/browser/injected.js"></script>`)
		})
		h.app.Web.HandleFunc("GET /test/browser/injected.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			io.WriteString(w, `window.fragmentScriptExecuted=true`)
		})

		ready := make(chan struct{})
		var readyOnce sync.Once
		h.app.Web.HandleFunc("POST /test/browser/ready", func(w http.ResponseWriter, r *http.Request) {
			readyOnce.Do(func() { close(ready) })
			w.WriteHeader(http.StatusNoContent)
		})
		result := make(chan string, 1)
		for _, mode := range []string{"htmx", "native"} {
			h.app.Web.HandleFunc("POST /test/browser/"+mode+"-form", func(w http.ResponseWriter, r *http.Request) {
				r.Body = http.MaxBytesReader(w, r.Body, 4096)
				report := "PASS"
				if err := r.ParseForm(); err != nil || r.PostForm.Get("message") != "A&B + café" {
					report = mode + " form: missing or incorrectly encoded fields"
				}
				wantHX := ""
				if mode == "htmx" {
					wantHX = "true"
				}
				if r.Header.Get("HX-Request") != wantHX {
					report = mode + " form: unexpected HX-Request header"
				}

				if mode == "native" {
					// The native POST completes the browser checks. A blocked form
					// never reaches this handler and cannot report a false success.
					w.WriteHeader(http.StatusNoContent)
					select {
					case result <- report:
					default:
					}
					return
				}
				if report != "PASS" {
					http.Error(w, report, http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				io.WriteString(w, `<p id="form-success">Form saved</p>`)
			})
		}
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
		browserLogs := &capturedLogs{}
		server := httptest.NewServer(browserDiagnosticHandler(h.handler, result, browserLogs))
		defer server.Close()

		runBrowser(t, server.URL+"/test/browser", ready, result, browserLogs.String)
	})
}

// Keep browser tests on the site's pinned HTMX version and retain its integrity check.
func localBrowserHTMX(t *testing.T, html string) (string, []byte) {
	t.Helper()
	match := regexp.MustCompile(`<script src="(https://unpkg.com/htmx.org@[^"\s]+)"\s+integrity="sha384-([^"]+)"`).FindStringSubmatch(html)
	if len(match) != 3 {
		t.Fatal("site layout must pin HTMX with a SHA-384 integrity hash")
	}
	script := downloadBrowserHTMX(t, match[1], match[2])
	html = strings.Replace(html, match[1], "/test/browser/htmx.js", 1)
	html = strings.Replace(html, "<head>", `<head><script src="/test/diagnostics/errors.js"></script>`, 1)
	return html, script
}

func runBrowser(t *testing.T, url string, ready <-chan struct{}, result <-chan string, logs func() string) {
	t.Helper()
	browser := os.Getenv("HS_TEST_FIREFOX")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	artifacts := browserArtifactDirectory(t)
	logPath := filepath.Join(artifacts, "firefox.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	profile := t.TempDir()
	cmd := exec.CommandContext(ctx, browser, "--headless", "--no-remote", "--profile", profile, url)
	// Log process startup, not HTTP headers or credential form values.
	cmd.Env = append(os.Environ(), "MOZ_LOG=timestamp,sync,ChildProcessLifecycle:5",
		"MOZ_LOG_FILE="+filepath.Join(artifacts, "process-launch"))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 2 * time.Second
	metadata := fmt.Sprintf("Test: %s\nBrowser executable: %s\nArguments: %q\nTMPDIR: %s\nStartup limit: 30s\nCheck limit: 15s\n", t.Name(), cmd.Path, cmd.Args, os.TempDir())
	t.Log(metadata)
	writeBrowserArtifact(t, artifacts, "launch.txt", metadata)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start browser: %v\n%s", err, browserDiagnostics(profile, logPath, logs()))
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
		writeBrowserArtifact(t, artifacts, "after-stop.txt", fmt.Sprintf("Browser PID: %d\nElapsed: %s\nBrowser exit: %v\n%s\n%s",
			cmd.Process.Pid, time.Since(started), exitErr, browserProcessSnapshot(), browserDiagnostics(profile, logPath, logs())))
		preserveBrowserCrashes(t, profile, artifacts)
	}()

	readyAfter, err := waitForBrowser(ready, result, exited, 30*time.Second, 15*time.Second)
	timing := fmt.Sprintf("Browser PID: %d\nReadiness observed after: %s (zero means not observed)\nElapsed: %s\nResult: %v\n", cmd.Process.Pid, readyAfter, time.Since(started), err)
	writeBrowserArtifact(t, artifacts, "timing.txt", timing)
	t.Log(timing)
	if err != nil {
		// Inspect the live processes before cancellation destroys the evidence.
		diagnostics := browserFailureSnapshot(url, profile, logPath, logs())
		writeBrowserArtifact(t, artifacts, "before-stop.txt", diagnostics)
		t.Fatalf("%v\n%s", err, diagnostics)
	}
}

// Readiness means the page's JavaScript has started, not just that Firefox exists.
// Time spent starting the browser and loading the page does not consume check time.
func waitForBrowser(ready <-chan struct{}, result <-chan string, exited <-chan struct{}, startupTimeout, checkTimeout time.Duration) (time.Duration, error) {
	started := time.Now()
	startup := time.NewTimer(startupTimeout)
	defer startup.Stop()
	select {
	case <-ready:
		startup.Stop()
	case report := <-result:
		if report != "PASS" {
			return 0, fmt.Errorf("browser checks failed before readiness was observed: %s", report)
		}
		// Both channels can be ready when a fast test completes. Accept success
		// only if the page has also reported readiness.
		select {
		case <-ready:
			return time.Since(started), nil
		default:
			return 0, fmt.Errorf("browser reported success before JavaScript reported readiness")
		}
	case <-exited:
		return 0, fmt.Errorf("browser exited during startup, before JavaScript reported readiness")
	case <-startup.C:
		return 0, fmt.Errorf("browser startup timed out after %s, before JavaScript reported readiness", startupTimeout)
	}

	readyAfter := time.Since(started)
	checks := time.NewTimer(checkTimeout)
	defer checks.Stop()
	select {
	case report := <-result:
		if report != "PASS" {
			return readyAfter, fmt.Errorf("browser checks failed: %s", report)
		}
		return readyAfter, nil
	case <-exited:
		return readyAfter, fmt.Errorf("browser exited during checks, after JavaScript reported readiness")
	case <-checks.C:
		return readyAfter, fmt.Errorf("browser checks timed out after %s, after JavaScript reported readiness", checkTimeout)
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
