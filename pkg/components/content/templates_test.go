package content

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

type countingTemplateFS struct {
	fs.FS
	reads atomic.Int64
}

func (f *countingTemplateFS) Open(name string) (fs.File, error) {
	f.reads.Add(1)
	return f.FS.Open(name)
}

func TestPreparedTemplatesConcurrentRequests(t *testing.T) {
	source := fstest.MapFS{
		"page.html":     {Data: []byte(`<html>{{.Title}}:{{renderPartial .PartialName .}}</html>`)},
		"fragment.html": {Data: []byte(`{{renderPartial .PartialName .}}`)},
		"body.html":     {Data: []byte(`{{define "body"}}{{.Data}}{{end}}{{define "notice"}}<aside hx-swap-oob="true">{{.Data}}</aside>{{end}}`)},
	}
	files := &countingTemplateFS{FS: source}
	cm := contentservice.NewContentManager()
	cm.AddPageLayout(files, "page.html")
	cm.AddHtmxLayout(files, "fragment.html")
	prepared, err := PrepareTemplates(cm, TemplatePath{FS: files, Path: "body.html"})
	if err != nil {
		t.Fatal(err)
	}
	reads := files.reads.Load()
	if reads == 0 {
		t.Fatal("preparation did not read templates")
	}
	// Prepared sets retain neither the file data nor the manager's template lists.
	source["body.html"].Data = []byte(`{{invalid`)
	cm.Layouts[contentservice.PageType] = nil
	t.Cleanup(func() {
		if got := files.reads.Load(); got != reads {
			t.Fatalf("requests reread files: reads=%d, want %d", got, reads)
		}
	})
	for i := range 40 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if i%2 == 0 {
				r.Header.Set("HX-Request", "true")
			}
			if i%4 < 2 {
				r.Header.Set("HX-History-Restore-Request", "true")
			}
			fragment := i%4 == 2
			c := NewManagedContent(r, cm)
			c.Templates, c.PartialName, c.Title = prepared, "body", "Title"
			c.Data = fmt.Sprintf("<request %d>", i)
			c.OutOfBand = []string{"notice"}
			w := httptest.NewRecorder()
			if err := c.Render(w, r); err != nil {
				t.Fatal(err)
			}
			escaped := fmt.Sprintf("&lt;request %d&gt;", i)
			want := "<html>Title:" + escaped + "</html>"
			if fragment {
				want = escaped + `<aside hx-swap-oob="true">` + escaped + "</aside>"
			}
			if got := w.Body.String(); got != want {
				t.Fatalf("body=%q, want %q", got, want)
			}
		})
	}
}

func TestPrepareTemplatesFailures(t *testing.T) {
	files := fstest.MapFS{"valid.html": {Data: []byte("valid")}, "broken.html": {Data: []byte("{{if}}")}}
	for _, tc := range []struct {
		name  string
		cm    *contentservice.ContentManagerService
		paths []TemplatePath
		cause error
	}{
		{name: "no templates"},
		{name: "nil filesystem", paths: []TemplatePath{{Path: "valid.html"}}},
		{name: "invalid path", paths: []TemplatePath{{FS: files, Path: "../valid.html"}}},
		{name: "missing file", paths: []TemplatePath{{FS: files, Path: "missing.html"}}, cause: fs.ErrNotExist},
		{name: "syntax", paths: []TemplatePath{{FS: files, Path: "broken.html"}}},
		{name: "fragment syntax", cm: &contentservice.ContentManagerService{Layouts: map[string][]TemplatePath{
			contentservice.PageType: {{FS: files, Path: "valid.html"}},
			contentservice.HtmxType: {{FS: files, Path: "broken.html"}},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := PrepareTemplates(tc.cm, tc.paths...)
			if prepared != nil || err == nil {
				t.Fatalf("prepare = %v, %v, want no partial result and an error", prepared, err)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatalf("lost underlying cause: %v", err)
			}
			if tc.name == "no templates" {
				if !errors.Is(err, ErrNoTemplates) {
					t.Fatalf("lost no-templates error type: %v", err)
				}
			} else {
				if !errors.Is(err, ErrParsingTemplates) {
					t.Fatalf("lost parsing error type: %v", err)
				}
			}
			if tc.name == "fragment syntax" && !strings.Contains(err.Error(), "fragment") {
				t.Fatalf("missing variant context: %v", err)
			}
		})
	}
}

func TestPreparedTemplatesFailureDoesNotCommit(t *testing.T) {
	failure := errors.New("private execution failure")
	for _, kind := range []string{"execution", "partial", "missing partial", "out of band", "mixed files", "zero value"} {
		t.Run(kind, func(t *testing.T) {
			text := `<p>prefix</p>{{.Data.Fail}}`
			if kind == "partial" {
				text = `prefix{{renderPartial "part" .}}{{define "part"}}{{.Data.Fail}}{{end}}`
			}
			if kind == "missing partial" {
				text = `prefix{{renderPartial "missing" .}}`
			}
			if kind == "out of band" {
				text = `success{{define "notice"}}{{.Data.Fail}}{{end}}`
			}
			files := fstest.MapFS{"body.html": {Data: []byte(text)}}
			prepared, err := PrepareTemplates(nil, TemplatePath{FS: files, Path: "body.html"})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("HX-Request", "true")
			c := NewContent(r)
			c.Templates, c.Data = prepared, failingTemplateData{err: failure}
			c.Headers = map[string]string{"HX-Redirect": "/success"}
			if kind == "out of band" {
				c.OutOfBand = []string{"notice"}
			}
			if kind == "mixed files" {
				c.AddContent(files, "body.html")
			}
			if kind == "zero value" {
				c.Templates = &Templates{}
			}
			w := &recordingResponse{ResponseRecorder: httptest.NewRecorder()}
			err = c.Render(w, r)
			if !errors.Is(err, ErrRenderingTemplates) || w.committed || w.Body.Len() != 0 || w.Header().Get("HX-Redirect") != "" {
				t.Fatalf("render failure: error=%v committed=%t body=%q headers=%v", err, w.committed, w.Body.String(), w.Header())
			}
			if (kind == "execution" || kind == "partial" || kind == "out of band") && !errors.Is(err, failure) {
				t.Fatalf("lost execution cause: %v", err)
			}
		})
	}
}

func TestPreparedTemplatesAreApplicationOwned(t *testing.T) {
	for _, value := range []string{"first", "second"} {
		files := fstest.MapFS{"body.html": {Data: []byte(value)}}
		prepared, err := PrepareTemplates(nil, TemplatePath{FS: files, Path: "body.html"})
		if err != nil {
			t.Fatal(err)
		}
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			c := NewContent(nil)
			c.Templates = prepared
			body, err := c.render()
			if err != nil || body.String() != value {
				t.Fatalf("body=%v error=%v", body, err)
			}
		})
	}
}

func TestPreparedTemplatesMatchPerRender(t *testing.T) {
	files := fstest.MapFS{
		"layout.html": {Data: []byte(`<main title="{{.Title}}">{{renderPartial .PartialName .}}</main>`)},
		"shared.html": {Data: []byte(`{{define "shared"}}original{{end}}`)},
		"body.html":   {Data: []byte(`{{define "body"}}{{template "shared" .}}<a href="{{.URL}}">{{.Data}}</a>{{end}}`)},
	}
	// The component uses the same basename in a different filesystem.
	components := fstest.MapFS{"shared.html": {Data: []byte(`{{define "shared"}}component{{end}}`)}}
	cm := contentservice.NewContentManager()
	for _, kind := range []string{contentservice.PageType, contentservice.HtmxType} {
		cm.AddLayout(kind, files, "layout.html")
		cm.AddContent(kind, files, "shared.html")
		cm.AddComponent(kind, components, "shared.html")
	}
	prepared, err := PrepareTemplates(cm, TemplatePath{FS: files, Path: "body.html"})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if fragment {
			r.Header.Set("HX-Request", "true")
		}
		c := NewManagedContent(r, cm)
		c.Title, c.PartialName, c.Data = `"<title>`, "body", "<script>unsafe</script>"
		c.URL = "javascript:alert(1)"
		c.AddContent(files, "body.html")
		original, err := c.render()
		if err != nil {
			t.Fatal(err)
		}
		c.Contents, c.Templates = nil, prepared
		body, err := c.render()
		if err != nil {
			t.Fatal(err)
		}
		want := `<main title="&#34;&lt;title&gt;">component<a href="#ZgotmplZ">&lt;script&gt;unsafe&lt;/script&gt;</a></main>`
		if body.String() != want || body.String() != original.String() {
			t.Fatalf("fragment=%t: prepared=%q original=%q", fragment, body.String(), original.String())
		}
	}
}
