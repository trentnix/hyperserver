package content

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

// BenchmarkRenderTemplates renders a layout, named partial, and escaped input.
// It includes buffering but excludes HTTP delivery and notification storage.
func BenchmarkRenderTemplates(b *testing.B) {
	files := fstest.MapFS{
		"page.html":     {Data: []byte(`<html><title>{{.Title}}</title>{{renderPartial .PartialName .}}</html>`)},
		"fragment.html": {Data: []byte(`{{renderPartial .PartialName .}}`)},
		"greeting.html": {Data: []byte(`{{define "greeting"}}<p>Hello, {{.Data}}!</p>{{end}}`)},
	}
	cm := contentservice.NewContentManager()
	cm.AddPageLayout(files, "page.html")
	cm.AddHtmxLayout(files, "fragment.html")
	prepared, err := PrepareTemplates(cm, TemplatePath{FS: files, Path: "greeting.html"})
	if err != nil {
		b.Fatal(err)
	}
	for _, variant := range []string{"page", "fragment"} {
		b.Run(variant+"/prepared", func(b *testing.B) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if variant == "fragment" {
				r.Header.Set("HX-Request", "true")
			}
			c := NewManagedContent(r, cm)
			c.Templates, c.Title, c.PartialName, c.Data = prepared, "Greeting", "greeting", "<visitor>"
			b.ReportAllocs()
			for b.Loop() {
				if _, err := c.render(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
