package content

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestRenderCacheHeaders(t *testing.T) {
	path := renderTemplateFile(t, "page.html", "<p>Rendered</p>")
	for _, tc := range []struct {
		name          string
		existingCache []string
		contentCache  string
		wantCache     []string
	}{
		{"safe default", nil, "", []string{"no-store"}},
		{"public opt-in", nil, "public, max-age=60", []string{"public, max-age=60"}},
		{"private opt-in", nil, "private, max-age=30", []string{"private, max-age=30"}},
		{"middleware policy", []string{"private, no-cache"}, "", []string{"private, no-cache"}},
		{"preserve no-store", []string{"no-store"}, "public, max-age=60", []string{"no-store"}},
		{"preserve multiple lines", []string{"private", "no-store"}, "public", []string{"private", "no-store"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, htmx := range []bool{false, true} {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				c := NewContent(r)
				c.AddContent(path)
				c.Headers = map[string]string{"vary": "Origin, HX-Request", "cache-control": tc.contentCache}
				w := httptest.NewRecorder()
				w.Header().Add("Vary", "Accept-Encoding")
				w.Header().Add("Vary", "Origin")
				for _, value := range tc.existingCache {
					w.Header().Add("Cache-Control", value)
				}
				if err := c.Render(w, r); err != nil {
					t.Fatal(err)
				}
				header := w.Result().Header // Assert headers at the time the body was committed.
				if got := header.Values("Cache-Control"); !reflect.DeepEqual(got, tc.wantCache) {
					t.Errorf("Cache-Control = %q, want %q", got, tc.wantCache)
				}
				if got, want := header.Values("Vary"), []string{"Accept-Encoding", "Origin", "HX-Request"}; !reflect.DeepEqual(got, want) {
					t.Errorf("Vary = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestRenderPreservesVaryWildcard(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := NewContent(r)
	c.AddContent(renderTemplateFile(t, "page.html", "rendered"))
	c.Headers = map[string]string{"Vary": "*"}
	w := httptest.NewRecorder()
	w.Header().Add("Vary", "Accept-Encoding")
	if err := c.Render(w, r); err != nil {
		t.Fatal(err)
	}
	if got, want := w.Result().Header.Values("Vary"), []string{"Accept-Encoding", "*"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Vary = %q, want %q", got, want)
	}
}

func TestRenderCacheableRepresentations(t *testing.T) {
	cm := contentservice.NewContentManager()
	cm.AddPageLayout(renderTemplateFile(t, "page.html", "<html><body>Public page</body></html>"))
	cm.AddHtmxLayout(renderTemplateFile(t, "fragment.html", "<p>Public fragment</p>"))
	for _, htmx := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/same-url", nil)
		want := "<html><body>Public page</body></html>"
		if htmx {
			r.Header.Set("HX-Request", "true")
			want = "<p>Public fragment</p>"
		}
		c := NewManagedContent(r, cm)
		c.Headers = map[string]string{"Cache-Control": "public, max-age=60"}
		w := httptest.NewRecorder()
		if err := c.Render(w, r); err != nil {
			t.Fatal(err)
		}
		if w.Body.String() != want || w.Result().Header.Get("Vary") != "HX-Request" || w.Result().Header.Get("Cache-Control") != "public, max-age=60" {
			t.Fatalf("incorrect cacheable representation: %v %q", w.Result().Header, w.Body.String())
		}
	}
}
