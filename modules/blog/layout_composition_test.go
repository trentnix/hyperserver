package blog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

// Applications can replace individual components or compose them in a different
// layout. Shared responses and module views must honor the same startup overrides.
func TestLayoutCompositionOverrides(t *testing.T) {
	files := fstest.MapFS{
		"components.html": {Data: []byte(`
{{define "blog.siteSidebar"}}<aside data-custom-sidebar>{{.LayoutData.Profile.Name}}</aside>{{end}}
{{define "blog.footerArchive"}}<span data-custom-footer>{{.LayoutData.Profile.Name}}</span>{{end}}
{{define "blog.setupPanel"}}<main data-custom-setup>{{renderPartial .PartialName .}}</main>{{end}}
`)},
		"layout.html": {Data: []byte(`<!DOCTYPE html><html><body data-custom-layout>
{{template "blog.siteSidebar" .}}{{template "blog.mainContent" .}}{{template "blog.siteFooter" .}}
</body></html>`)},
	}
	for _, tc := range []struct {
		name                           string
		fragment, history, full, setup bool
		shared, layout                 bool
	}{
		{name: "page"},
		{name: "fragment", fragment: true},
		{name: "history", history: true},
		{name: "full HTMX", full: true},
		{name: "setup", setup: true},
		{name: "shared error", shared: true},
		{name: "alternative layout", layout: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm := contentservice.NewContentManager()
			if err := (&Site{}).configureRendering(cm, nil, false); err != nil {
				t.Fatal(err)
			}
			cm.BuildLayoutData = func(*http.Request) any {
				return navigation{Profile: Profile{Name: "Site <name>"}, Setup: tc.setup}
			}
			cm.AddPageComponent(files, "components.html")
			cm.AddHtmxComponent(files, "components.html")
			if tc.layout {
				cm.RegisterLayouts(contentservice.PageType, files, "layout.html")
			}
			if err := cm.PrepareTemplates(); err != nil {
				t.Fatal(err)
			}
			templates, err := prepareTemplates(cm)
			if err != nil {
				t.Fatal(err)
			}
			view, err := templates.View(cm, "blog.message")
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.fragment || tc.history || tc.full {
				r.Header.Set("HX-Request", "true")
				r.Header.Set("HX-Target", "div#article-content")
			}
			if tc.history {
				r.Header.Set("HX-History-Restore-Request", "true")
			}
			if tc.full {
				r.Header.Set("HX-Request-Type", "full")
			}
			w := httptest.NewRecorder()
			status := http.StatusOK
			if tc.shared {
				status = http.StatusServiceUnavailable
				cm.HandleError(w, r, "Saved <message>", nil, status)
			} else {
				page := view.Content(r, "Saved <message>")
				page.OutOfBand = []string{"blog.recentUpdates"}
				if err := page.Render(w, r); err != nil {
					t.Fatal(err)
				}
			}
			requireStatus(t, w, status)
			body := w.Body.String()
			for marker, want := range map[string]bool{
				"data-custom-sidebar": !tc.fragment && !tc.setup,
				"data-custom-footer":  !tc.setup,
				"data-custom-setup":   tc.setup,
				"data-custom-layout":  tc.layout,
			} {
				count := 0
				if want {
					count = 1
				}
				if got := strings.Count(body, marker); got != count {
					t.Fatalf("%s count = %d, want %d: %s", marker, got, count, body)
				}
			}
			if !strings.Contains(body, "Saved &lt;message&gt;") || strings.Contains(body, "Site <name>") {
				t.Fatal("component composition lost escaped request data", body)
			}
			if tc.fragment && !strings.Contains(body, `hx-swap-oob="innerHTML:#post-archive"`) {
				t.Fatal("fragment lost the composed footer update")
			}
		})
	}
}
