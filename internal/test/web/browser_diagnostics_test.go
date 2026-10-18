package web_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// Load this separately, before the test script, so syntax errors in that script
// can be reported along with uncaught exceptions and rejected promises.
const browserErrorScript = `
function reportBrowserError(message) {
  fetch("/test/diagnostics/error", {method: "POST", body: message})
    .catch(error => console.error("Could not report browser error", error));
}
window.addEventListener("error", event => {
  const message = event.message
    ? event.message + " at " + event.filename + ":" + event.lineno + ":" + event.colno
    : "Could not load " + (event.target.src || event.target.href || event.target.tagName);
  reportBrowserError(message);
}, true);
window.addEventListener("unhandledrejection", event => {
  reportBrowserError("Unhandled rejection: " + String(event.reason));
});
`

// Record browser traffic independently of application logging. Some browser
// tests use a standalone mux, and middleware can reject requests before logging.
func browserDiagnosticHandler(next http.Handler, result chan<- string, logs *capturedLogs) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test/diagnostics/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /test/diagnostics/errors.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		io.WriteString(w, browserErrorScript)
	})
	mux.HandleFunc("POST /test/diagnostics/error", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil || strings.TrimSpace(string(body)) == "" {
			http.Error(w, "invalid browser error report", http.StatusBadRequest)
			return
		}
		select {
		case result <- "JavaScript error: " + string(body):
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record arrival before dispatch, so a stalled handler remains visible.
		logs.mu.Lock()
		logs.lines = append(logs.lines, fmt.Sprintf("%s %s %s", time.Now().Format(time.RFC3339Nano), r.Method, r.URL.Path))
		logs.mu.Unlock()
		mux.ServeHTTP(w, r)
	})
}

func TestBrowserDiagnosticHandler(t *testing.T) {
	logs := &capturedLogs{}
	result := make(chan string, 1)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(logs.String(), "POST /form") {
			t.Error("request must be logged before dispatch")
		}
		w.WriteHeader(http.StatusAccepted)
	})
	handler := browserDiagnosticHandler(next, result, logs)
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/test/diagnostics/health", nil))
	if health.Code != http.StatusNoContent || len(result) != 0 {
		t.Fatalf("health probe affected the browser result: status=%d, results=%d", health.Code, len(result))
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/form?secret=query-secret", strings.NewReader("body-secret")))
	if w.Code != http.StatusAccepted || strings.Contains(logs.String(), "secret") {
		t.Fatalf("status=%d logs=%s", w.Code, logs.String())
	}

	for _, body := range []string{"", " \n", strings.Repeat("x", 4097)} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/test/diagnostics/error", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest || len(result) != 0 {
			t.Fatalf("invalid report: status=%d results=%d", w.Code, len(result))
		}
	}
	for range 2 {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/test/diagnostics/error", strings.NewReader("script failed")))
		if w.Code != http.StatusNoContent {
			t.Fatalf("error report: status=%d", w.Code)
		}
	}
	if report := <-result; report != "JavaScript error: script failed" {
		t.Fatalf("report=%q", report)
	}
}

func TestBrowserServerProbe(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusServiceUnavailable, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/test/diagnostics/health" || r.URL.RawQuery != "" {
					t.Errorf("unexpected probe target: %s", r.URL)
				}
				w.Header().Set("Location", "/must-not-follow")
				w.WriteHeader(status)
			}))
			defer server.Close()
			if got := probeBrowserServer(server.URL + "/test?secret=value"); got != fmt.Sprintf("%d %s", status, http.StatusText(status)) {
				t.Fatal("unexpected probe result:", got)
			}
			server.Close()
			if got := probeBrowserServer(server.URL); !strings.Contains(got, "connect") {
				t.Fatal("closed server appeared healthy:", got)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	if got := probeBrowserServer(server.URL); !strings.Contains(got, "context deadline exceeded") {
		t.Fatal("stalled probe did not time out:", got)
	}
}

func TestBrowserErrorReporting(t *testing.T) {
	if os.Getenv("HS_TEST_FIREFOX") == "" {
		t.Skip("set HS_TEST_FIREFOX to a Firefox executable to run browser tests")
	}
	// One browser checks all four error paths. No readiness request is sent:
	// these failures must be visible even when the test script cannot start.
	reports := make(chan string, 4)
	logs := &capturedLogs{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<script src="/test/diagnostics/errors.js"></script><script src="/syntax.js"></script><script src="/exception.js"></script><script src="/rejection.js"></script><script src="/missing.js"></script>`)
	})
	for path, script := range map[string]string{
		"/syntax.js":    "const = ;",
		"/exception.js": `throw new Error("diagnostic exception");`,
		"/rejection.js": `Promise.reject(new Error("diagnostic rejection"));`,
	} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			io.WriteString(w, script)
		})
	}
	server := httptest.NewServer(browserDiagnosticHandler(mux, reports, logs))
	defer server.Close()

	// Translate the expected failures into a result for the browser runner.
	ready := make(chan struct{})
	result := make(chan string, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		var received []string
		for range 4 {
			select {
			case report := <-reports:
				received = append(received, report)
			case <-done:
				return
			}
		}
		close(ready)
		all := strings.Join(received, "\n")
		for _, want := range []string{"SyntaxError", "diagnostic exception", "Unhandled rejection: Error: diagnostic rejection", "Could not load " + server.URL + "/missing.js"} {
			if !strings.Contains(all, want) {
				result <- "missing " + want + ": " + all
				return
			}
		}
		result <- "PASS"
	}()
	runBrowser(t, server.URL, ready, result, logs.String)
}

func TestWaitForBrowser(t *testing.T) {
	for _, tc := range []struct {
		name                                        string
		readyAfter, reportAfter, exitAfter, elapsed time.Duration
		report, wantErr                             string
	}{
		{name: "success", readyAfter: time.Second, reportAfter: 2 * time.Second, elapsed: 2 * time.Second, report: "PASS"},
		{name: "startup beyond old limit", readyAfter: 40 * time.Second, reportAfter: 41 * time.Second, elapsed: 41 * time.Second, report: "PASS"},
		{name: "slow startup leaves full check budget", readyAfter: 59 * time.Second, reportAfter: 73 * time.Second, elapsed: 73 * time.Second, report: "PASS"},
		{name: "startup timeout", elapsed: 60 * time.Second, wantErr: "browser startup timed out after 1m0s"},
		{name: "script failure before readiness", reportAfter: time.Second, elapsed: time.Second, report: "JavaScript error: script failed", wantErr: "browser checks failed before readiness was observed: JavaScript error: script failed"},
		{name: "success requires readiness", reportAfter: time.Second, elapsed: time.Second, report: "PASS", wantErr: "browser reported success before JavaScript reported readiness"},
		{name: "check timeout", readyAfter: 5 * time.Second, elapsed: 20 * time.Second, wantErr: "browser checks timed out after 15s"},
		{name: "startup exit", exitAfter: time.Second, elapsed: time.Second, wantErr: "browser exited during startup"},
		{name: "check exit", readyAfter: time.Second, exitAfter: 2 * time.Second, elapsed: 2 * time.Second, wantErr: "browser exited during checks"},
		{name: "assertion failure", readyAfter: time.Second, reportAfter: 2 * time.Second, elapsed: 2 * time.Second, report: "HTMX did not load", wantErr: "browser checks failed: HTMX did not load"},
		{name: "empty report is not success", readyAfter: time.Second, reportAfter: 2 * time.Second, elapsed: 2 * time.Second, wantErr: "browser checks failed:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ready, exited := make(chan struct{}), make(chan struct{})
				result := make(chan string, 1)
				if tc.readyAfter > 0 {
					go func() {
						time.Sleep(tc.readyAfter)
						close(ready)
					}()
				}
				if tc.reportAfter > 0 {
					go func() {
						time.Sleep(tc.reportAfter)
						result <- tc.report
					}()
				}
				if tc.exitAfter > 0 {
					go func() {
						time.Sleep(tc.exitAfter)
						close(exited)
					}()
				}

				started := time.Now()
				readyAfter, err := waitForBrowser(ready, result, exited, browserStartupTimeout, browserCheckTimeout)
				if readyAfter != tc.readyAfter {
					t.Fatalf("readiness observed after %s, want %s", readyAfter, tc.readyAfter)
				}
				if tc.wantErr == "" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v, want %q", err, tc.wantErr)
				}
				if elapsed := time.Since(started); elapsed != tc.elapsed {
					t.Fatalf("elapsed=%s, want %s", elapsed, tc.elapsed)
				}
			})
		})
	}
}

func TestWaitForBrowserAlreadyFinished(t *testing.T) {
	for range 100 {
		ready := make(chan struct{})
		close(ready)
		result := make(chan string, 1)
		result <- "PASS"
		if _, err := waitForBrowser(ready, result, make(chan struct{}), time.Second, time.Second); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBrowserDiagnostics(t *testing.T) {
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte("test profile"), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "firefox.log")
	if err := os.WriteFile(logPath, []byte("browser startup message"), 0600); err != nil {
		t.Fatal(err)
	}
	diagnostics := browserDiagnostics(profile, logPath, "GET /test/browser")
	for _, want := range []string{profile, "prefs.js", "browser startup message", "GET /test/browser"} {
		if !strings.Contains(diagnostics, want) {
			t.Errorf("diagnostics missing %q: %s", want, diagnostics)
		}
	}

	missing := filepath.Join(t.TempDir(), "missing")
	diagnostics = browserDiagnostics(missing, missing, "")
	for _, want := range []string{"Profile entries (read error:", "Browser output (read error:", "no such file or directory", "HTTP requests:"} {
		if !strings.Contains(diagnostics, want) {
			t.Errorf("missing-file diagnostics omitted %q: %s", want, diagnostics)
		}
	}
}
