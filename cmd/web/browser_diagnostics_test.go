package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
