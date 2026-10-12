package form

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

type responseForm struct {
	Form
	Name string
}

func (f *responseForm) Bind(*http.Request) error { return nil }
func (f *responseForm) Validate() FieldErrors    { return nil }

func TestFormErrorResponseDispatch(t *testing.T) {
	files := fstest.MapFS{
		"page.html":     {Data: []byte(`<html>{{renderPartial "form" .}}</html>`)},
		"fragment.html": {Data: []byte(`{{renderPartial "form" .}}`)},
		"form.html":     {Data: []byte(`{{define "form"}}<form>{{.Data.Name}}:{{range .Data.GetFieldErrors "Name"}}{{.}}{{end}}</form>{{end}}`)},
	}
	for _, hx := range []bool{false, true} {
		for _, status := range []int{http.StatusOK, http.StatusUnprocessableEntity} {
			t.Run(fmt.Sprintf("htmx=%t/status=%d", hx, status), func(t *testing.T) {
				r := httptest.NewRequest(http.MethodPost, "/", nil)
				r.Header.Set(htmx.HeaderRequest, fmt.Sprint(hx))
				cm := contentservice.NewContentManager()
				cm.AddPageLayout(files, "page.html")
				cm.AddHtmxLayout(files, "fragment.html")
				c := content.NewManagedContent(r, cm)
				c.AddContent(files, "form.html")
				c.ResponseStatusCode = status
				c.HTMX.Response = &htmx.Response{Retarget: "#form", Reswap: "outerHTML"}
				f := &responseForm{Name: "<input>"}
				f.SetFieldErrors(FieldErrors{"Name": {"<invalid>"}})
				w := httptest.NewRecorder()
				HandleFormError(w, r, c, f, "", nil)

				want := "<form>&lt;input&gt;:&lt;invalid&gt;</form>"
				retarget, reswap := "#form", "outerHTML"
				if !hx {
					want = "<html>" + want + "</html>"
					retarget, reswap = "", ""
				}
				h := w.Result().Header
				if w.Code != status || w.Body.String() != want || h.Get("HX-Retarget") != retarget || h.Get("HX-Reswap") != reswap {
					t.Fatalf("form response = %d %v %q", w.Code, h, w.Body.String())
				}
			})
		}
	}
}
