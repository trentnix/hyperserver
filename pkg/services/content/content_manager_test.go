package content

import (
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestManagerAddsTemplatePaths(t *testing.T) {
	first := fstest.MapFS{"first.html": {Data: []byte("first")}}
	second := fstest.MapFS{"second.html": {Data: []byte("second")}}
	for _, tc := range []struct {
		name    string
		kind    string
		add     func(*ContentManagerService, fs.FS, string)
		addMany func(*ContentManagerService, fs.FS, ...string)
		paths   func(*ContentManagerService) map[string][]TemplatePath
	}{
		{"custom layouts", "custom",
			func(c *ContentManagerService, files fs.FS, name string) { c.AddLayout("custom", files, name) },
			func(c *ContentManagerService, files fs.FS, names ...string) { c.AddLayouts("custom", files, names...) },
			func(c *ContentManagerService) map[string][]TemplatePath { return c.Layouts }},
		{"page layouts", PageType, (*ContentManagerService).AddPageLayout, (*ContentManagerService).AddPageLayouts, func(c *ContentManagerService) map[string][]TemplatePath { return c.Layouts }},
		{"htmx layouts", HtmxType, (*ContentManagerService).AddHtmxLayout, (*ContentManagerService).AddHtmxLayouts, func(c *ContentManagerService) map[string][]TemplatePath { return c.Layouts }},
		{"custom contents", "custom",
			func(c *ContentManagerService, files fs.FS, name string) { c.AddContent("custom", files, name) },
			func(c *ContentManagerService, files fs.FS, names ...string) { c.AddContents("custom", files, names...) },
			func(c *ContentManagerService) map[string][]TemplatePath { return c.Contents }},
		{"page contents", PageType, (*ContentManagerService).AddPageContent, (*ContentManagerService).AddPageContents, func(c *ContentManagerService) map[string][]TemplatePath { return c.Contents }},
		{"htmx contents", HtmxType, (*ContentManagerService).AddHtmxContent, (*ContentManagerService).AddHtmxContents, func(c *ContentManagerService) map[string][]TemplatePath { return c.Contents }},
		{"custom components", "custom",
			func(c *ContentManagerService, files fs.FS, name string) { c.AddComponent("custom", files, name) },
			func(c *ContentManagerService, files fs.FS, names ...string) {
				c.AddComponents("custom", files, names...)
			},
			func(c *ContentManagerService) map[string][]TemplatePath { return c.Components }},
		{"page components", PageType, (*ContentManagerService).AddPageComponent, (*ContentManagerService).AddPageComponents, func(c *ContentManagerService) map[string][]TemplatePath { return c.Components }},
		{"htmx components", HtmxType, (*ContentManagerService).AddHtmxComponent, (*ContentManagerService).AddHtmxComponents, func(c *ContentManagerService) map[string][]TemplatePath { return c.Components }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm := NewContentManager()
			tc.add(cm, first, "first.html")
			tc.addMany(cm, second, "second.html", "third.html")
			tc.addMany(cm, first)
			want := map[string][]TemplatePath{tc.kind: {
				{FS: first, Path: "first.html"},
				{FS: second, Path: "second.html"},
				{FS: second, Path: "third.html"},
			}}
			if got := tc.paths(cm); !reflect.DeepEqual(got, want) {
				t.Fatalf("paths = %#v, want %#v", got, want)
			}
		})
	}
}

func TestManagerReplacesTemplatePaths(t *testing.T) {
	first := fstest.MapFS{"old.html": {Data: []byte("old")}}
	second := fstest.MapFS{"new.html": {Data: []byte("new")}}
	for _, tc := range []struct {
		name     string
		register func(*ContentManagerService, string, fs.FS, ...string)
		paths    func(*ContentManagerService) map[string][]TemplatePath
	}{
		{"layouts", (*ContentManagerService).RegisterLayouts, func(c *ContentManagerService) map[string][]TemplatePath { return c.Layouts }},
		{"contents", (*ContentManagerService).RegisterContents, func(c *ContentManagerService) map[string][]TemplatePath { return c.Contents }},
		{"components", (*ContentManagerService).RegisterComponents, func(c *ContentManagerService) map[string][]TemplatePath { return c.Components }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm := NewContentManager()
			tc.register(cm, PageType, first, "old.html")
			tc.register(cm, HtmxType, first, "old.html")
			tc.register(cm, PageType, second, "new.html", "next.html")
			want := map[string][]TemplatePath{
				PageType: {{FS: second, Path: "new.html"}, {FS: second, Path: "next.html"}},
				HtmxType: {{FS: first, Path: "old.html"}},
			}
			if got := tc.paths(cm); !reflect.DeepEqual(got, want) {
				t.Fatalf("replacement paths = %#v, want %#v", got, want)
			}
			tc.register(cm, PageType, second)
			want[PageType] = nil
			if got := tc.paths(cm); !reflect.DeepEqual(got, want) {
				t.Fatalf("cleared paths = %#v, want %#v", got, want)
			}
		})
	}
}
