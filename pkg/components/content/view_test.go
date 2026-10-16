package content

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/pkg/components/messages"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestPrepareViewValidation(t *testing.T) {
	for _, name := range []string{"missing file", "syntax", "empty partial", "unknown partial", "page only", "fragment only"} {
		t.Run(name, func(t *testing.T) {
			files := fstest.MapFS{
				"page.html":     {Data: []byte(`{{renderPartial .PartialName .}}{{define "page-only"}}page{{end}}`)},
				"fragment.html": {Data: []byte(`{{renderPartial .PartialName .}}{{define "fragment-only"}}fragment{{end}}`)},
				"body.html":     {Data: []byte(`{{define "body"}}{{.Data}}{{end}}`)},
			}
			cm := contentservice.NewContentManager()
			cm.AddPageLayout(files, "page.html")
			cm.AddHtmxLayout(files, "fragment.html")
			partial, file := "body", "body.html"
			var cause error
			var detail string
			switch name {
			case "missing file":
				file, cause = "missing.html", fs.ErrNotExist
			case "syntax":
				files[file].Data = []byte(`{{if}}`)
				cause = ErrParsingTemplates
			case "empty partial":
				partial = ""
			case "unknown partial":
				partial, detail = "missing", "page partial template not found: missing"
			case "page only":
				partial, detail = "page-only", "fragment partial template not found: page-only"
			case "fragment only":
				partial, detail = "fragment-only", "page partial template not found: fragment-only"
			}
			view, err := PrepareView(cm, partial, TemplatePath{FS: files, Path: file})
			if err == nil || view != nil {
				t.Fatalf("invalid view: %v, %v", view, err)
			}
			if cause != nil && !errors.Is(err, cause) {
				t.Fatalf("lost cause: %v", err)
			}
			if detail != "" && !strings.Contains(err.Error(), detail) {
				t.Fatalf("missing variant and partial context: %v", err)
			}
		})
	}
	for _, templates := range []*Templates{nil, {}} {
		if view, err := templates.View(nil, "body"); err == nil || view != nil {
			t.Fatalf("unprepared templates accepted: %v, %v", view, err)
		}
	}
}

func TestViewsShareTemplatesNotRequestState(t *testing.T) {
	files := &countingTemplateFS{FS: fstest.MapFS{
		"page.html":     {Data: []byte(`<main>{{.Title}}:{{.LayoutData}}:{{renderPartial .PartialName .}}</main>`)},
		"fragment.html": {Data: []byte(`{{renderPartial .PartialName .}}`)},
		"body.html":     {Data: []byte(`{{define "first"}}one:{{template "value" .}}{{end}}{{define "second"}}two:{{template "value" .}}{{end}}{{define "value"}}{{.Data}}{{end}}`)},
	}}
	cm := contentservice.NewContentManager()
	cm.AppTitle = "Default"
	cm.BuildLayoutData = func(r *http.Request) any { return r.URL.Path }
	cm.AddPageLayout(files, "page.html")
	cm.AddHtmxLayout(files, "fragment.html")
	templates, err := PrepareTemplates(cm, TemplatePath{FS: files, Path: "body.html"})
	if err != nil {
		t.Fatal(err)
	}
	reads := files.reads.Load()
	first, err := templates.View(cm, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := templates.View(cm, "second")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if got := files.reads.Load(); got != reads {
			t.Fatalf("view creation or rendering reread files: %d, want %d", got, reads)
		}
	})
	for i := range 24 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			view, prefix := first, "one:"
			if i%2 == 1 {
				view, prefix = second, "two:"
			}
			r := httptest.NewRequest("GET", fmt.Sprintf("/request-%d", i), nil)
			if i%3 != 0 {
				r.Header.Set("HX-Request", "true")
			}
			if i%3 == 2 {
				r.Header.Set("HX-History-Restore-Request", "true")
			}
			data := fmt.Sprintf("<data-%d>", i)
			page := view.Content(r, data)
			page.Title = fmt.Sprintf("Title-%d", i)
			page.ResponseStatusCode = http.StatusAccepted
			page.Headers = map[string]string{"X-View": prefix}
			w := httptest.NewRecorder()
			if err := page.Render(w, r); err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("%s&lt;data-%d&gt;", prefix, i)
			if i%3 != 1 {
				want = "<main>" + page.Title + ":" + r.URL.Path + ":" + want + "</main>"
			}
			if w.Body.String() != want || w.Code != http.StatusAccepted || w.Header().Get("X-View") != prefix {
				t.Fatalf("response: %d %v %q, want %q", w.Code, w.Header(), w.Body.String(), want)
			}
			next := view.Content(nil, nil)
			if next.Title != "Default" || next.LayoutData != nil || next.Data != nil || next.Headers != nil || next.ResponseStatusCode != http.StatusOK {
				t.Fatal("request fields leaked into another response", next)
			}
		})
	}
}

func TestViewUsesExistingResponseBehavior(t *testing.T) {
	files := fstest.MapFS{"view.html": {Data: []byte(`{{range .Notifications}}{{.Message}}{{end}}{{renderPartial .PartialName .}}{{define "body"}}{{.Data}}{{end}}{{define "failure"}}prefix{{.Data.Fail}}{{end}}{{define "notice"}}<aside hx-swap-oob="true">notice</aside>{{end}}`)}}
	cm := contentservice.NewContentManager()
	loads, acknowledgments := 0, 0
	cm.LoadNotifications = func(*http.Request) ([]messages.Notification, func(http.ResponseWriter) error, error) {
		loads++
		return []messages.Notification{messages.NewNotification("<notification>", messages.NotificationTypeSuccess)}, func(http.ResponseWriter) error {
			acknowledgments++
			return nil
		}, nil
	}
	view, err := PrepareView(cm, "body", TemplatePath{FS: files, Path: "view.html"})
	if err != nil {
		t.Fatal(err)
	}
	for _, hx := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/", nil)
		if hx {
			r.Header.Set("HX-Request", "true")
		}
		page := view.Content(r, "<data>")
		page.OutOfBand = []string{"notice"}
		w := httptest.NewRecorder()
		if err := page.Render(w, r); err != nil {
			t.Fatal(err)
		}
		want := "&lt;notification&gt;&lt;data&gt;"
		if hx {
			want += `<aside hx-swap-oob="true">notice</aside>`
		}
		if w.Body.String() != want {
			t.Fatalf("rendered %q, want %q", w.Body.String(), want)
		}

		page = view.Content(r, nil)
		page.RedirectURL = "/next"
		w = httptest.NewRecorder()
		if err := page.Render(w, r); err != nil {
			t.Fatal(err)
		}
		status, header := http.StatusSeeOther, "Location"
		if hx {
			status, header = http.StatusOK, "HX-Redirect"
		}
		if w.Code != status || w.Header().Get(header) != "/next" {
			t.Fatalf("redirect: %d %v", w.Code, w.Header())
		}
	}
	if loads != 2 || acknowledgments != 2 {
		t.Fatalf("notification delivery: loaded=%d acknowledged=%d", loads, acknowledgments)
	}

	failure := errors.New("private execution failure")
	r := httptest.NewRequest("GET", "/", nil)
	page := view.Content(r, failingTemplateData{err: failure})
	page.PartialName = "failure" // The returned Content still supports explicit overrides.
	w := &recordingResponse{ResponseRecorder: httptest.NewRecorder()}
	if err := page.Render(w, r); !errors.Is(err, failure) || w.committed || w.Body.Len() != 0 || acknowledgments != 2 {
		t.Fatalf("failed render: %v, committed=%t, acknowledgments=%d", err, w.committed, acknowledgments)
	}
}

func TestViewWithoutManager(t *testing.T) {
	files := fstest.MapFS{"view.html": {Data: []byte(`{{renderPartial .PartialName .}}{{define "body"}}{{.Data}}{{end}}`)}}
	view, err := PrepareView(nil, "body", TemplatePath{FS: files, Path: "view.html"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	page := view.Content(r, "<value>")
	w := httptest.NewRecorder()
	if err := page.Render(w, r); err != nil || w.Body.String() != "&lt;value&gt;" || page.ContentManager != nil {
		t.Fatalf("unmanaged view: error=%v body=%q", err, w.Body.String())
	}
	page = new(View).Content(r, nil)
	w = httptest.NewRecorder()
	if err := page.Render(w, r); err == nil || w.Body.Len() != 0 {
		t.Fatal("zero view rendered successfully")
	}
}
