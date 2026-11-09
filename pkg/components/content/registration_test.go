package content

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

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
		prepareContent(t, page, TemplatePath{FS: pages, Path: "body.html"}, TemplatePath{FS: pages, Path: "heading.html"})
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
