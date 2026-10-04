package main

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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
	runHTTPScenario(t, func(h *httpHarness) {
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

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		logPath := filepath.Join(t.TempDir(), "firefox.log")
		logFile, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		defer logFile.Close()
		cmd := exec.CommandContext(ctx, browser, "--headless", "--no-remote", "--profile", t.TempDir(), server.URL+"/test/browser")
		cmd.Stdout, cmd.Stderr = logFile, logFile
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = 2 * time.Second
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
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

		select {
		case report := <-result:
			if report != "PASS" {
				t.Fatalf("browser checks failed: %s", report)
			}
		case <-exited:
			output, _ := os.ReadFile(logPath)
			t.Fatalf("browser exited before reporting results: %v\n%s", exitErr, output)
		case <-ctx.Done():
			output, _ := os.ReadFile(logPath)
			t.Fatalf("browser checks timed out: %s\nHTTP requests:\n%s", output, h.logs.String())
		}
	})
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
