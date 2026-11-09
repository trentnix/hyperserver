package content

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestRenderUsesIndependentFilesystems(t *testing.T) {
	t.Chdir(t.TempDir())
	layouts := fstest.MapFS{"layout.html": {Data: []byte(`<main>{{renderPartial .PartialName .}}</main>`)}}
	for _, value := range []string{"first", "second"} {
		pages := fstest.MapFS{"page.html": {Data: []byte(`{{define "page"}}` + value + `:{{.Data}}{{end}}`)}}
		page := NewContent(nil)
		page.PartialName, page.Data = "page", "<script>"
		prepareContent(t, page, TemplatePath{FS: layouts, Path: "layout.html"}, TemplatePath{FS: pages, Path: "page.html"})
		w := httptest.NewRecorder()
		if err := page.Render(w, httptest.NewRequest(http.MethodGet, "/", nil)); err != nil {
			t.Fatal(err)
		}
		if got := w.Body.String(); got != "<main>"+value+":&lt;script&gt;</main>" {
			t.Fatal("rendered another filesystem's template:", got)
		}
	}
}

func TestRenderTemplateOrderAcrossFilesystems(t *testing.T) {
	page := NewContent(nil)
	prepareContent(t, page,
		TemplatePath{FS: fstest.MapFS{"page.html": {Data: []byte("first")}}, Path: "page.html"},
		TemplatePath{FS: fstest.MapFS{"page.html": {Data: []byte("second")}}, Path: "page.html"},
	)
	w := httptest.NewRecorder()
	if err := page.Render(w, httptest.NewRequest(http.MethodGet, "/", nil)); err != nil || w.Body.String() != "second" {
		t.Fatalf("template order: body=%q error=%v", w.Body.String(), err)
	}
}
