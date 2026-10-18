package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestBrowserHistory(t *testing.T) {
	if os.Getenv("HS_TEST_FIREFOX") == "" {
		t.Skip("set HS_TEST_FIREFOX to a Firefox executable to run browser tests")
	}

	runSiteScenario(t, func(h *httpHarness) {
		file := testTemplateFile(t, "modules/site/testdata/history.html")
		templates, err := content.PrepareTemplates(h.app.ContentManager, file)
		if err != nil {
			t.Fatal(err)
		}
		render := func(w http.ResponseWriter, r *http.Request, name string) error {
			c := content.NewManagedContent(r, h.app.ContentManager)
			c.Templates, c.PartialName, c.Data = templates, "test.browser.history", name
			if name == "b" || name == "d" {
				c.HTMX.Response = &htmx.Response{PushURL: "/test/history/" + name}
			} else if name == "c" {
				c.HTMX.Response = &htmx.Response{ReplaceURL: "/test/history/c"}
			}
			// Model a page that now requires login when revisited through history.
			if name == "d" && r.Header.Get(htmx.HeaderHistoryRestoreRequest) == "true" {
				c.RedirectURL = "/test/history/login"
			}
			return c.Render(w, r)
		}
		initial := httptest.NewRecorder()
		r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/test/history/a", nil), h.sessions)
		if err := render(initial, r, "a"); err != nil {
			t.Fatal(err)
		}
		html := browserTestPage(initial.Body.String())

		ready := make(chan struct{})
		result := make(chan string, 1)
		var readyOnce sync.Once
		report := func(message string) {
			select {
			case result <- message:
			default:
			}
		}
		h.app.Web.HandleFunc("POST /test/history-ready", func(w http.ResponseWriter, r *http.Request) {
			readyOnce.Do(func() { close(ready) })
			w.WriteHeader(http.StatusNoContent)
		})
		h.app.Web.HandleFunc("POST /test/history-result", func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
			if err != nil {
				report("invalid browser result")
			} else {
				report(string(body))
			}
			w.WriteHeader(http.StatusNoContent)
		})
		h.app.Web.HandleFunc("GET /test/history-run.js", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join(h.app.Config.App.WorkingDirectory, "modules/site/testdata/history.js"))
		})
		h.app.Web.HandleFunc("GET /test/history/{page}", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(htmx.HeaderRequest) == "" && r.PathValue("page") == "a" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				io.WriteString(w, html)
				return
			}
			response := httptest.NewRecorder()
			if err := render(response, r, r.PathValue("page")); err != nil {
				report("could not render history response: " + err.Error())
			}
			isPage := strings.Contains(strings.ToLower(response.Body.String()), "<!doctype html>")
			if response.Code == http.StatusOK && isPage != (r.Header.Get(htmx.HeaderHistoryRestoreRequest) == "true") {
				report("history response used the wrong page/fragment templates")
			}
			for key, values := range response.Header() {
				w.Header()[key] = values
			}
			w.WriteHeader(response.Code)
			w.Write(response.Body.Bytes())
		})

		logs := &capturedLogs{}
		server := httptest.NewServer(browserDiagnosticHandler(h.handler, result, logs))
		defer server.Close()
		runBrowser(t, server.URL+"/test/history/a", ready, result, logs.String)
	})
}
