package content

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/trentnix/hyperserver/pkg/components/messages"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/util"
)

func renderTemplateFile(t *testing.T, name, text string) TemplatePath {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return TemplatePath(path)
}

type failingTemplateData struct{ err error }

func (d failingTemplateData) Fail() (string, error) { return "", d.err }

// Track explicit and implicit commits, including an empty Write.
type recordingResponse struct {
	*httptest.ResponseRecorder
	committed bool
}

func (w *recordingResponse) WriteHeader(status int) {
	w.committed = true
	w.ResponseRecorder.WriteHeader(status)
}

func (w *recordingResponse) Write(body []byte) (int, error) {
	w.committed = true
	return w.ResponseRecorder.Write(body)
}

func TestRenderFailureDoesNotCommitResponse(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		for _, failure := range []string{"no templates", "missing file", "parse", "execution", "partial execution", "missing partial"} {
			t.Run(fmt.Sprintf("%s/htmx=%t", failure, htmx), func(t *testing.T) {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				c := NewContent(r)
				c.ResponseStatusCode = http.StatusCreated
				c.Headers = map[string]string{"Content-Type": "text/html", "Content-Length": "999", "HX-Redirect": "/success"}
				privateErr := errors.New("private rendering failure")
				c.Data = failingTemplateData{err: privateErr}
				text := "<p>partial output</p>{{.Data.Fail}}"
				switch failure {
				case "parse":
					text = "<p>partial output</p>{{"
				case "partial execution":
					text = `<p>outer prefix</p>{{renderPartial "part" .}}{{define "part"}}<p>inner prefix</p>{{.Data.Fail}}{{end}}`
				case "missing partial":
					text = `<p>outer prefix</p>{{renderPartial "missing" .}}`
				}
				switch failure {
				case "no templates":
				case "missing file":
					c.AddContent(TemplatePath(filepath.Join(t.TempDir(), "missing.html")))
				default:
					c.AddContent(renderTemplateFile(t, "content.html", text))
				}

				w := &recordingResponse{ResponseRecorder: httptest.NewRecorder()}
				w.Header().Set("Cache-Control", "no-store")
				err := c.Render(w, r)
				if err == nil || w.committed || w.Body.Len() != 0 {
					t.Fatalf("failed render committed output: error=%v, committed=%t, body=%q", err, w.committed, w.Body.String())
				}
				if (failure == "execution" || failure == "partial execution") && !errors.Is(err, privateErr) {
					t.Fatal("lost template execution error:", err)
				}
				for key := range c.Headers {
					if got := w.Header().Get(key); got != "" {
						t.Fatalf("failed render applied %s: %q", key, got)
					}
				}
				util.HttpError(w, r, "Unable to render", nil, http.StatusInternalServerError)
				response := w.Result()
				if response.StatusCode != http.StatusInternalServerError || w.Body.String() != "Unable to render\n" || response.Header.Get("Content-Type") != "text/plain; charset=utf-8" || response.Header.Get("Cache-Control") != "no-store" {
					t.Fatalf("fallback response: status=%d, headers=%v, body=%q", response.StatusCode, response.Header, w.Body.String())
				}
			})
		}
	}
}

func TestRenderBufferedPageAndFragment(t *testing.T) {
	cm := contentservice.NewContentManager()
	cm.RegisterLayouts(contentservice.PageType, []TemplatePath{renderTemplateFile(t, "page.html", `<html>{{renderPartial .PartialName .}}</html>`)})
	cm.RegisterLayouts(contentservice.HtmxType, []TemplatePath{renderTemplateFile(t, "fragment.html", `{{renderPartial .PartialName .}}`)})
	part := renderTemplateFile(t, "part.html", `{{define "part"}}<p>{{.Data}}</p>{{end}}`)
	for _, htmx := range []bool{false, true} {
		for _, status := range []int{0, http.StatusOK, http.StatusCreated, http.StatusUnprocessableEntity} {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if htmx {
				r.Header.Set("HX-Request", "true")
			}
			c := NewManagedContent(r, cm)
			c.AddContent(part)
			c.PartialName, c.Data = "part", "<script>unsafe</script>"
			c.ResponseStatusCode = status
			c.Headers = map[string]string{"Content-Type": "text/html; charset=utf-8", "X-Rendered": "yes"}
			w := httptest.NewRecorder()
			if err := c.Render(w, r); err != nil {
				t.Fatal(err)
			}
			want := "<p>&lt;script&gt;unsafe&lt;/script&gt;</p>"
			if !htmx {
				want = "<html>" + want + "</html>"
			}
			wantStatus := status
			if wantStatus == 0 {
				wantStatus = http.StatusOK
			}
			response := w.Result()
			if response.StatusCode != wantStatus || w.Body.String() != want || response.Header.Get("X-Rendered") != "yes" || response.Header.Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("htmx=%t, status=%d: got status=%d, headers=%v, body=%q", htmx, status, response.StatusCode, response.Header, w.Body.String())
			}
		}
	}
}

type failingResponse struct {
	*httptest.ResponseRecorder
	err error
}

func (w failingResponse) Write(body []byte) (int, error) { return len(body) / 2, w.err }

func TestRenderReportsDeliveryFailure(t *testing.T) {
	writeErr := errors.New("connection lost")
	for _, err := range []error{writeErr, nil} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		c := NewContent(r)
		c.AddContent(renderTemplateFile(t, "content.html", "<p>complete page</p>"))
		w := failingResponse{ResponseRecorder: httptest.NewRecorder(), err: err}
		want := err
		if want == nil {
			want = io.ErrShortWrite
		}
		if err := c.Render(w, r); !errors.Is(err, want) {
			t.Fatalf("delivery error = %v, want %v", err, want)
		}
	}
}

func TestRenderReportsNotificationReadFailure(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if htmx {
			r.Header.Set("HX-Request", "true")
		}
		cm := contentservice.NewContentManager()
		cm.RenderNotifications = true
		c := NewManagedContent(r, cm)
		c.ResponseStatusCode = http.StatusCreated
		c.Headers = map[string]string{"X-Rendered": "yes"}
		w := &recordingResponse{ResponseRecorder: httptest.NewRecorder()}
		var retrieval *messages.ErrRetrievingContentMessages
		if err := c.Render(w, r); !errors.As(err, &retrieval) {
			t.Fatalf("missing session manager error = %v", err)
		}
		if w.committed || w.Body.Len() != 0 || w.Header().Get("X-Rendered") != "" || len(c.Notifications) != 0 {
			t.Fatal("notification read failure committed output or changed notifications")
		}
	}
}
