package content

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"testing/fstest"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestAddTemplatePaths(t *testing.T) {
	first := fstest.MapFS{"first.html": {Data: []byte("first")}}
	second := fstest.MapFS{"second.html": {Data: []byte("second")}}
	for _, tc := range []struct {
		name  string
		add   func(*Content, fs.FS, ...string)
		paths func(*Content) []TemplatePath
	}{
		{"layouts", (*Content).AddLayout, func(c *Content) []TemplatePath { return c.Layouts }},
		{"contents", (*Content).AddContent, func(c *Content) []TemplatePath { return c.Contents }},
		{"components", (*Content).AddComponent, func(c *Content) []TemplatePath { return c.Components }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewContent(nil)
			tc.add(c, first)
			if len(tc.paths(c)) != 0 {
				t.Fatal("adding no names created template paths")
			}
			tc.add(c, first, "first.html")
			names := []string{"second.html", "third.html"}
			tc.add(c, second, names...)
			names[0] = "changed.html"
			tc.add(c, first)
			want := []TemplatePath{
				{FS: first, Path: "first.html"},
				{FS: second, Path: "second.html"},
				{FS: second, Path: "third.html"},
			}
			if got := tc.paths(c); !reflect.DeepEqual(got, want) {
				t.Fatalf("paths = %#v, want %#v", got, want)
			}
		})
	}
}

func TestRenderRegisteredFilesystems(t *testing.T) {
	layouts := fstest.MapFS{
		"page.html":     {Data: []byte("<main>{{template \"body\" .}}</main>")},
		"fragment.html": {Data: []byte("{{template \"body\" .}}")},
	}
	pages := fstest.MapFS{
		"body.html":    {Data: []byte("{{define \"body\"}}{{template \"heading\" .}}{{template \"footer\" .}}{{end}}")},
		"heading.html": {Data: []byte("{{define \"heading\"}}<h1>{{.Data}}</h1>{{end}}")},
	}
	components := fstest.MapFS{
		"footer.html": {Data: []byte("{{define \"footer\"}}<footer>Site</footer>{{end}}")},
	}
	cm := contentservice.NewContentManager()
	cm.AddPageLayout(layouts, "page.html")
	cm.AddHtmxLayout(layouts, "fragment.html")
	cm.AddPageComponent(components, "footer.html")
	cm.AddHtmxComponent(components, "footer.html")

	for _, htmx := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if htmx {
			r.Header.Set("HX-Request", "true")
		}
		page := NewManagedContent(r, cm)
		page.AddContent(pages, "body.html", "heading.html")
		page.Data = "<unsafe>"
		w := httptest.NewRecorder()
		if err := page.Render(w, r); err != nil {
			t.Fatal(err)
		}
		want := "<h1>&lt;unsafe&gt;</h1><footer>Site</footer>"
		if !htmx {
			want = "<main>" + want + "</main>"
		}
		if got := w.Body.String(); got != want {
			t.Fatalf("htmx=%t: body = %q, want %q", htmx, got, want)
		}
	}
}
