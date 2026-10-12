package content

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestRenderCacheHeaders(t *testing.T) {
	files, name := renderTemplateFile(t, "page.html", "<p>Rendered</p>")
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
				c.AddContent(files, name)
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
				if got, want := header.Values("Vary"), []string{"Accept-Encoding", "Origin", "HX-Request", "HX-History-Restore-Request"}; !reflect.DeepEqual(got, want) {
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
	for _, tc := range []struct {
		name     string
		request  string
		restore  string
		fragment bool
	}{
		{"page", "", "", false},
		{"fragment", "true", "", true},
		{"history with HX-Request", "true", "true", false},
		{"history without HX-Request", "", "true", false},
		{"false history header", "true", "false", true},
		{"noncanonical history header", "true", "TRUE", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/same-url", nil)
			r.Header.Set("HX-Request", tc.request)
			r.Header.Set("HX-History-Restore-Request", tc.restore)
			want := "<html><body>Public page</body></html>"
			if tc.fragment {
				want = "<p>Public fragment</p>"
			}
			c := NewManagedContent(r, cm)
			if c.IsHtmx() != tc.fragment {
				t.Fatal("incorrect response classification")
			}
			c.HTMX.Response = &htmx.Response{PushURL: "/next"}
			c.Headers = map[string]string{"Cache-Control": "public, max-age=60"}
			w := httptest.NewRecorder()
			if err := c.Render(w, r); err != nil {
				t.Fatal(err)
			}
			if w.Body.String() != want || w.Result().Header.Get("Vary") != "HX-Request" || w.Result().Header.Get("Cache-Control") != "public, max-age=60" {
				t.Fatalf("incorrect cacheable representation: %v %q", w.Result().Header, w.Body.String())
			}
			if got, want := w.Result().Header.Values("Vary"), []string{"HX-Request", "HX-History-Restore-Request"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("Vary = %q, want %q", got, want)
			}
			if (w.Header().Get("HX-Push-Url") != "") != tc.fragment {
				t.Fatal("fragment metadata applied to the wrong representation")
			}
		})
	}
}

func TestHistoryRestoreIgnoresFragmentDirectives(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/page", nil)
	r.Header.Set("HX-Request", "true")
	r.Header.Set("HX-History-Restore-Request", "true")
	// Render must use the actual request, not the classification at construction.
	c := NewContent(nil)
	c.AddContent(renderTemplateFile(t, "page.html", "<html>Restored</html>"))
	c.HTMX.Response = &htmx.Response{NoContent: true, Trigger: "ignored", Retarget: "#ignored"}
	c.OutOfBand = []string{"missing-fragment"}
	w := httptest.NewRecorder()
	if err := c.Render(w, r); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || w.Body.String() != "<html>Restored</html>" || w.Header().Get("HX-Trigger") != "" || w.Header().Get("HX-Retarget") != "" {
		t.Fatalf("history response = %d %v %q", w.Code, w.Header(), w.Body.String())
	}
}
