package content

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/trentnix/hyperserver/pkg/components/messages"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/util"
)

func renderTemplateFile(t *testing.T, name, text string) (fs.FS, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return os.DirFS(filepath.Dir(path)), filepath.Base(path)
}

type failingTemplateData struct{ err error }

func TestTemplateRenderingDoesNotLoadNotifications(t *testing.T) {
	cm := contentservice.NewContentManager()
	cm.LoadNotifications = func(http.ResponseWriter, *http.Request) ([]messages.Notification, error) {
		t.Fatal("template rendering must not call the notification loader")
		return nil, nil
	}
	c := NewManagedContent(httptest.NewRequest(http.MethodGet, "/", nil), cm)
	c.Notifications = []messages.Notification{messages.NewNotification("<provided>", messages.NotificationTypeSuccess)}
	c.AddContent(renderTemplateFile(t, "notifications.html", `{{range .Notifications}}{{.Message}}{{end}}`))
	body, err := c.render()
	if err != nil {
		t.Fatal(err)
	}
	if body.String() != "&lt;provided&gt;" {
		t.Fatalf("rendered notifications = %q", body.String())
	}
}

func TestManagedContentLayoutData(t *testing.T) {
	cm := contentservice.NewContentManager()
	cm.BuildLayoutData = func(r *http.Request) any {
		return map[string]string{"path": r.URL.Path}
	}
	files, layoutName := renderTemplateFile(t, "layout.html", `{{.LayoutData.path}}:{{.Data}}`)
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/"+name, nil)
			c := NewManagedContent(r, cm)
			c.Data = "<page data>"
			c.AddLayout(files, layoutName)
			w := httptest.NewRecorder()
			if err := c.Render(w, r); err != nil {
				t.Fatal(err)
			}
			if got, want := w.Body.String(), "/"+name+":&lt;page data&gt;"; got != want {
				t.Fatalf("rendered %q, want %q", got, want)
			}
		})
	}
	if c := NewManagedContent(nil, cm); c.LayoutData != nil {
		t.Fatal("nil request should not invoke the layout data callback")
	}
	if c := NewManagedContent(httptest.NewRequest(http.MethodGet, "/", nil), contentservice.NewContentManager()); c.LayoutData != nil {
		t.Fatal("layout data should be optional")
	}
}

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
				c.Headers = map[string]string{"Content-Type": "text/html", "Content-Length": "999", "HX-Redirect": "/success", "Vary": "Origin"}
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
					c.AddContent(os.DirFS(t.TempDir()), "missing.html")
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
	cm.AddPageLayout(renderTemplateFile(t, "page.html", `<html>{{renderPartial .PartialName .}}</html>`))
	cm.AddHtmxLayout(renderTemplateFile(t, "fragment.html", `{{renderPartial .PartialName .}}`))
	files, name := renderTemplateFile(t, "part.html", `{{define "part"}}<p>{{.Data}}</p>{{end}}`)
	for _, htmx := range []bool{false, true} {
		for _, status := range []int{0, http.StatusOK, http.StatusCreated, http.StatusUnprocessableEntity} {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if htmx {
				r.Header.Set("HX-Request", "true")
			}
			c := NewManagedContent(r, cm)
			c.AddContent(files, name)
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
	failure := errors.New("notification source unavailable")
	for _, htmx := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if htmx {
			r.Header.Set("HX-Request", "true")
		}
		cm := contentservice.NewContentManager()
		cm.LoadNotifications = func(http.ResponseWriter, *http.Request) ([]messages.Notification, error) {
			return []messages.Notification{messages.NewNotification("must not render", messages.NotificationTypeError)}, failure
		}
		c := NewManagedContent(r, cm)
		c.ResponseStatusCode = http.StatusCreated
		c.Headers = map[string]string{"X-Rendered": "yes"}
		w := &recordingResponse{ResponseRecorder: httptest.NewRecorder()}
		if err := c.Render(w, r); !errors.Is(err, failure) {
			t.Fatalf("notification loader error = %v", err)
		}
		if w.committed || w.Body.Len() != 0 || w.Header().Get("X-Rendered") != "" || len(c.Notifications) != 0 {
			t.Fatal("notification read failure committed output or changed notifications")
		}
	}
}

func TestRenderLoadsNotificationsWithoutSessionServices(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		for _, load := range []bool{false, true} {
			t.Run(fmt.Sprintf("htmx=%t/load=%t", htmx, load), func(t *testing.T) {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				cm := contentservice.NewContentManager()
				calls := 0
				if load {
					cm.LoadNotifications = func(gotW http.ResponseWriter, gotR *http.Request) ([]messages.Notification, error) {
						calls++
						if gotW != w || gotR != r {
							t.Error("loader did not receive the render's request and writer")
						}
						return []messages.Notification{messages.NewNotification("<remote>", messages.NotificationTypeSuccess)}, nil
					}
				}
				page := NewManagedContent(r, cm)
				page.Notifications = []messages.Notification{messages.NewNotification("local", messages.NotificationTypeError)}
				page.AddContent(renderTemplateFile(t, "notifications.html", `{{range .Notifications}}{{.Message}}:{{.IsSuccess}}|{{end}}`))
				if err := page.Render(w, r); err != nil {
					t.Fatal(err)
				}
				want, wantCalls := "local:false|", 0
				if load {
					want += "&lt;remote&gt;:true|"
					wantCalls = 1
				}
				if w.Body.String() != want || calls != wantCalls || len(w.Result().Cookies()) != 0 {
					t.Fatalf("body=%q calls=%d cookies=%v", w.Body.String(), calls, w.Result().Cookies())
				}
			})
		}
	}
}
