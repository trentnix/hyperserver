package main

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/services/middleware"
)

func TestHTTPSiteErrorResponses(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.app.Web.HandleFunc("GET /test/site-error", func(w http.ResponseWriter, r *http.Request) {
			status, _ := strconv.Atoi(r.URL.Query().Get("status"))
			var err error
			if r.URL.Query().Get("failure") == "true" {
				err = errors.New("safe internal diagnostic detail")
			}
			h.app.ContentManager.HandleError(w, r, r.URL.Query().Get("message"), err, status)
		})
		for _, htmx := range []bool{false, true} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				for _, status := range []int{400, 401, 403, 404, 409, 422, 500, 503, 0, 200, 700} {
					message := `<script>alert("public message")</script>`
					if status == http.StatusServiceUnavailable {
						message = ""
					}
					params := url.Values{"status": {strconv.Itoa(status)}, "message": {message}, "failure": {strconv.FormatBool(status == 500)}, "token": {"private-query-token"}}
					w := h.request(method, "/test/site-error?"+params.Encode(), nil, htmx)
					wantStatus := status
					if status < 400 || status > 599 {
						wantStatus = http.StatusInternalServerError
					}
					if w.Code != wantStatus || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Header().Get("Cache-Control") != "no-store" {
						t.Fatalf("status=%d headers=%v, want %d HTML", w.Code, w.Header(), wantStatus)
					}
					if method == http.MethodHead {
						if w.Body.Len() != 0 {
							t.Fatal("HEAD returned an error body")
						}
					} else {
						if message == "" {
							message = http.StatusText(wantStatus)
						}
						if !strings.Contains(w.Body.String(), template.HTMLEscapeString(message)) || strings.Contains(w.Body.String(), "safe internal diagnostic detail") {
							t.Fatalf("unexpected public error body: %s", w.Body.String())
						}
						if strings.Contains(w.Body.String(), "<!DOCTYPE html>") == htmx {
							t.Fatal("wrong page/fragment response")
						}
					}
					if len(w.Result().Cookies()) != 0 || w.Header().Get("HX-Redirect") != "" || w.Header().Get("Location") != "" {
						t.Fatal("error response wrote a session cookie or redirected")
					}
					logged := 0
					requestID := w.Header().Get("X-Request-ID")
					if requestID == "" {
						t.Fatal("error response has no request ID")
					}
					for _, line := range strings.Split(h.logs.String(), "\n") {
						if strings.Contains(line, "Request Error") && strings.Contains(line, requestID) {
							logged++
							if !strings.Contains(line, "safe internal diagnostic detail") {
								t.Fatal("error log lost diagnostic details")
							}
						}
					}
					wantLogs := 0
					if status == 500 {
						wantLogs = 1
					}
					if logged != wantLogs {
						t.Fatalf("error logs=%d, want %d", logged, wantLogs)
					}
				}
			}
		}
		if strings.Contains(h.logs.String(), "private-query-token") || !h.app.ContentManager.RenderNotifications {
			t.Fatal("error handling leaked a query token or changed notification configuration")
		}
	})
}

func TestHTTPSiteErrorWithoutSession(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		// Bypass identity loading: the error presenter must work without its services.
		for _, htmx := range []bool{false, true} {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Cookie", "hs-message-session=corrupt")
			if htmx {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			h.app.ContentManager.HandleError(w, r, "Storage unavailable", nil, http.StatusServiceUnavailable)
			if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "Storage unavailable") || len(w.Result().Cookies()) != 0 {
				t.Fatalf("error rendering depended on sessions: %d %v %s", w.Code, w.Header(), w.Body.String())
			}
		}
	})
}

func TestHTTPSiteQueuedErrors(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		h.app.Web.HandleFunc("POST /test/queue-errors", func(w http.ResponseWriter, r *http.Request) {
			if err := messages.AddErrorNotification(w, r, "Queued error"); err != nil {
				t.Fatal(err)
			}
			if err := messages.AddSuccessNotification(w, r, "Queued success"); err != nil {
				t.Fatal(err)
			}
		})
		for _, htmx := range []bool{false, true} {
			h.request(http.MethodPost, "/test/queue-errors", nil, htmx)
			w := h.request(http.MethodGet, "/error", nil, htmx)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Queued error") || strings.Contains(w.Body.String(), "Queued success") {
				t.Fatalf("queued errors: status=%d body=%s", w.Code, w.Body.String())
			}
			w = h.request(http.MethodGet, "/error", nil, htmx)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "No errors were specified.") {
				t.Fatalf("empty error queue: status=%d body=%s", w.Code, w.Body.String())
			}
		}

		// Bypass session middleware to exercise failed notification retrieval.
		w := httptest.NewRecorder()
		h.app.Web.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/error", nil))
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Unable to load error notifications") {
			t.Fatalf("notification failure: status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestHTTPSiteErrorRenderFailure(t *testing.T) {
	for _, failure := range []string{"missing template", "invalid template", "execution failure"} {
		t.Run(failure, func(t *testing.T) {
			runHTTPScenario(t, func(h *httpHarness) {
				path := filepath.Join(t.TempDir(), "error-layout.html")
				if failure != "missing template" {
					source := "{{"
					if failure == "execution failure" {
						source = `partial-error-output{{ index .Data 99 }}`
					}
					if err := os.WriteFile(path, []byte(source), 0600); err != nil {
						t.Fatal(err)
					}
				}
				for _, kind := range []string{"page", "htmx"} {
					h.app.ContentManager.Layouts[kind] = []content.TemplatePath{content.TemplatePath(path)}
				}
				h.app.Web.HandleFunc("GET /test/broken-error-page", func(w http.ResponseWriter, r *http.Request) {
					h.app.ContentManager.HandleError(w, r, "Not allowed", nil, http.StatusForbidden)
				})
				for _, htmx := range []bool{false, true} {
					w := h.request(http.MethodGet, "/test/broken-error-page", nil, htmx)
					if w.Code != http.StatusInternalServerError || w.Body.String() != "Unable to display the error page\n" || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
						t.Fatalf("error fallback: %d %v %q", w.Code, w.Header(), w.Body.String())
					}
					if len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), path) {
						t.Fatal("error fallback leaked template details or wrote cookies")
					}
				}
				if !strings.Contains(h.logs.String(), filepath.Base(path)) {
					t.Fatal("template failure was not logged")
				}
			})
		})
	}
}

type failedErrorWriter struct {
	*httptest.ResponseRecorder
	writes int
}

func (w *failedErrorWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("error response connection closed")
}

func TestHTTPSiteErrorWriteFailure(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		handler := middleware.LoggerMiddleware(&testLogger{logs: h.logs})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.app.ContentManager.HandleError(w, r, "Unavailable", nil, http.StatusServiceUnavailable)
		}))
		w := &failedErrorWriter{ResponseRecorder: httptest.NewRecorder()}
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusServiceUnavailable || w.writes != 1 || !strings.Contains(h.logs.String(), "error response connection closed") {
			t.Fatalf("write failure: status=%d writes=%d logs=%s", w.Code, w.writes, h.logs.String())
		}
	})
}
