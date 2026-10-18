package web_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

type browserResponseForm struct {
	form.Form
	Email string
}

func (f *browserResponseForm) Bind(r *http.Request) error {
	f.Email = r.PostForm.Get("email")
	return nil
}

func (f *browserResponseForm) Validate() form.FieldErrors {
	if !form.ValidEmail(f.Email) {
		return form.FieldErrors{"Email": {"Enter a valid email address."}}
	}
	return nil
}

// Exercise the dispatcher through the site's real HTMX version and security policy.
func TestBrowserResponseDispatch(t *testing.T) {
	if os.Getenv("HS_TEST_FIREFOX") == "" {
		t.Skip("set HS_TEST_FIREFOX to a Firefox executable to run browser tests")
	}
	runSiteScenario(t, func(h *httpHarness) {
		file := testTemplateFile(t, "modules/site/testdata/response-dispatch.html")
		r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), h.sessions)
		page := content.NewManagedContent(r, h.app.ContentManager)
		page.PartialName = "test.browser.dispatch"
		page.AddContent(file.FS, file.Path)
		w := httptest.NewRecorder()
		if err := page.Render(w, r); err != nil {
			t.Fatal(err)
		}
		html := browserTestPage(w.Body.String())

		ready := make(chan struct{})
		result := make(chan string, 1)
		report := func(message string) {
			select {
			case result <- message:
			default:
			}
		}
		var readyOnce sync.Once
		var updates, redirects, destinations atomic.Int32
		h.app.Web.HandleFunc("POST /test/responses/ready", func(w http.ResponseWriter, r *http.Request) {
			readyOnce.Do(func() { close(ready) })
			w.WriteHeader(http.StatusNoContent)
		})
		h.app.Web.HandleFunc("POST /test/responses/result", func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
			if err != nil {
				report("invalid browser result")
			} else {
				report(string(body))
			}
			w.WriteHeader(http.StatusNoContent)
		})
		h.app.Web.HandleFunc("GET /test/responses", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, html)
		})
		h.app.Web.HandleFunc("GET /test/responses/run.js", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join(h.app.Config.App.WorkingDirectory, "modules/site/testdata/response-dispatch.js"))
		})
		h.app.Web.HandleFunc("POST /test/responses/update", func(w http.ResponseWriter, r *http.Request) {
			updates.Add(1)
			if r.Header.Get("HX-Request") != "true" {
				report("update was not an HTMX request")
			}
			f := &browserResponseForm{}
			var fields form.FieldErrors
			if err := form.ParseAndValidate(r, f); err != nil && !errors.As(err, &fields) {
				report("could not parse update form")
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			c := content.NewManagedContent(r, h.app.ContentManager)
			c.AddContent(file.FS, file.Path)
			c.PartialName = "test.browser.editor"
			c.Data = f
			c.HTMX.Response = &htmx.Response{Retarget: "#response-editor", Reswap: "outerHTML settle:0"}
			c.OutOfBand = []string{"test.browser.notice"}
			if f.HasErrors() {
				if r.PostForm.Get("status") == "422" {
					c.ResponseStatusCode = http.StatusUnprocessableEntity
				}
				c.Notifications = []messages.Notification{messages.NewNotification("<invalid>", messages.NotificationTypeError)}
				form.HandleFormError(w, r, c, f, "", nil)
				return
			}
			c.Notifications = []messages.Notification{messages.NewNotification("<saved>", messages.NotificationTypeSuccess)}
			if err := c.Render(w, r); err != nil {
				report("could not render update response")
			}
		})
		h.app.Web.HandleFunc("POST /test/responses/redirect", func(w http.ResponseWriter, r *http.Request) {
			redirects.Add(1)
			if err := r.ParseForm(); err != nil || r.PostForm.Get("secret") != "do-not-replay" || r.Header.Get("HX-Request") != "true" {
				report("redirect did not receive the expected HTMX POST")
			}
			c := content.NewContent(r)
			c.RedirectURL = "/test/responses/destination?from=post"
			if err := c.Render(w, r); err != nil {
				report("could not send redirect")
			}
		})
		// Accept any method so an accidental POST replay reports a specific failure.
		h.app.Web.HandleFunc("/test/responses/destination", func(w http.ResponseWriter, r *http.Request) {
			destinations.Add(1)
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
			if err != nil || len(body) != 0 || r.Method != http.MethodGet || r.URL.RawQuery != "from=post" || r.Header.Get("HX-Request") != "" {
				report("redirect destination did not receive a bodyless, non-HTMX GET")
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, `<!doctype html><html><head><script src="/test/diagnostics/errors.js"></script><script src="/test/responses/run.js" defer></script></head><body><p id="destination">Arrived</p></body></html>`)
		})

		logs := &capturedLogs{}
		server := httptest.NewServer(browserDiagnosticHandler(h.handler, result, logs))
		defer server.Close()
		runBrowser(t, server.URL+"/test/responses", ready, result, logs.String)
		if updates.Load() != 3 || redirects.Load() != 1 || destinations.Load() != 1 {
			t.Fatalf("request counts: updates=%d, redirect POSTs=%d, destination requests=%d", updates.Load(), redirects.Load(), destinations.Load())
		}
	})
}
