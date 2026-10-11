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
		page.AddLayout(layouts, "layout.html")
		page.AddContent(pages, "page.html")
		w := httptest.NewRecorder()
		if err := page.Render(w, httptest.NewRequest(http.MethodGet, "/", nil)); err != nil {
			t.Fatal(err)
		}
		if got := w.Body.String(); got != "<main>"+value+":&lt;script&gt;</main>" {
			t.Fatal("rendered another filesystem's template:", got)
		}
	}
}

func TestRenderRejectsInvalidTemplateSources(t *testing.T) {
	for _, file := range []TemplatePath{
		{Path: "page.html"},
		{FS: fstest.MapFS{}, Path: "../page.html"},
		{FS: fstest.MapFS{}, Path: "/page.html"},
		{FS: fstest.MapFS{}, Path: ""},
		{FS: fstest.MapFS{}, Path: "missing.html"},
	} {
		page := NewContent(nil)
		page.AddContent(file.FS, file.Path)
		w := httptest.NewRecorder()
		if err := page.Render(w, httptest.NewRequest(http.MethodGet, "/", nil)); err == nil || w.Body.Len() != 0 {
			t.Fatalf("invalid source %q: error=%v body=%q", file.Path, err, w.Body.String())
		}
	}
}

func TestRenderTemplateOrderAcrossFilesystems(t *testing.T) {
	page := NewContent(nil)
	page.AddContent(fstest.MapFS{"page.html": {Data: []byte("first")}}, "page.html")
	page.AddContent(fstest.MapFS{"page.html": {Data: []byte("second")}}, "page.html")
	w := httptest.NewRecorder()
	if err := page.Render(w, httptest.NewRequest(http.MethodGet, "/", nil)); err != nil || w.Body.String() != "second" {
		t.Fatalf("template order: body=%q error=%v", w.Body.String(), err)
	}
}
