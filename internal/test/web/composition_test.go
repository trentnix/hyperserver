package web_test

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

// The application owns this contribution contract. Modules supply named templates
// from independent filesystems. The composing module depends on all loaded providers.
type templateContribution struct {
	Name  string
	Files []content.TemplatePath
}

type componentModule struct{ contribution *templateContribution }

func (*componentModule) Init(context.Context, *server.ApplicationServer) error { return nil }
func (*componentModule) Routes(*routing.Routes) error                          { return nil }
func (m *componentModule) Capabilities() map[string]any {
	return map[string]any{"fixture.components": m.contribution}
}

type composedComponent struct {
	Name string
	Data any
}

type compositionModule struct {
	contributions []*templateContribution
	files         content.TemplatePath
	manager       *contentservice.ContentManagerService
	layouts       map[string]*content.Templates
	available     map[string]bool
	routesBound   bool
}

func (m *compositionModule) BindDependencies(d handlers.Dependencies) error {
	var err error
	m.contributions, err = handlers.GetDependencies[*templateContribution](d, "fixture.components")
	return err
}

func (m *compositionModule) Init(_ context.Context, app *server.ApplicationServer) error {
	m.manager = app.ContentManager
	m.available = make(map[string]bool)
	files := []content.TemplatePath{m.files}
	for _, contribution := range m.contributions {
		if contribution.Name == "" || m.available[contribution.Name] {
			return fmt.Errorf("component names must be nonempty and unique: %q", contribution.Name)
		}
		m.available[contribution.Name] = true
		files = append(files, contribution.Files...)
	}
	templates, err := content.PrepareTemplates(m.manager, files...)
	if err != nil {
		return err
	}
	for _, name := range append([]string{"fixture.composition"}, m.componentNames()...) {
		if _, err := templates.View(m.manager, name); err != nil {
			return err
		}
	}
	compact, err := templates.WithLayouts("fixture.compact", "")
	if err != nil {
		return err
	}
	m.layouts = map[string]*content.Templates{"": templates, "compact": compact}
	return nil
}

func (m *compositionModule) componentNames() []string {
	names := make([]string, 0, len(m.contributions))
	for _, contribution := range m.contributions {
		names = append(names, contribution.Name)
	}
	return names
}

func (m *compositionModule) Routes(routes *routing.Routes) error {
	m.routesBound = true
	routes.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		templates := m.layouts[query.Get("layout")]
		if templates == nil {
			http.NotFound(w, r)
			return
		}
		names, specified := query["component"]
		if !specified {
			names = m.componentNames()
		}
		var components []composedComponent
		for _, name := range names {
			if !m.available[name] {
				http.NotFound(w, r)
				return
			}
			components = append(components, composedComponent{Name: name, Data: query.Get("value")})
		}
		page := content.NewManagedContent(r, m.manager)
		page.Templates, page.PartialName, page.Data = templates, "fixture.composition", components
		if partial := query.Get("partial"); partial != "" {
			if !m.available[partial] {
				http.NotFound(w, r)
				return
			}
			page.PartialName, page.Data = partial, query.Get("value")
		}
		if err := page.Render(w, r); err != nil {
			page.HandleRenderError(w, r, "Unable to render components", err)
		}
	})
	return routes.Err()
}

func TestHTTPModuleComposition(t *testing.T) {
	for _, loaded := range [][]string{nil, {"one"}, {"one", "two"}, {"two", "one"}} {
		t.Run(strings.Join(loaded, "+"), func(t *testing.T) {
			app, err := server.NewApplicationServer(config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { app.Shutdown() })
			var sources []*startupTemplateFS
			file := func(name, text string) content.TemplatePath {
				source := &startupTemplateFS{FS: fstest.MapFS{name: {Data: []byte(text)}}}
				sources = append(sources, source)
				return content.TemplatePath{FS: source, Path: name}
			}
			page := file("page.html", `<main>{{renderPartial .PartialName .}}</main>`)
			compact := file("compact.html", `{{define "fixture.compact"}}<article>{{renderPartial .PartialName .}}</article>{{end}}`)
			fragment := file("fragment.html", `{{renderPartial .PartialName .}}`)
			app.ContentManager.AddPageLayout(page.FS, page.Path)
			app.ContentManager.AddPageLayout(compact.FS, compact.Path)
			app.ContentManager.AddHtmxLayout(fragment.FS, fragment.Path)
			site := &compositionModule{files: file("composition.html", `{{define "fixture.composition"}}{{range .Data}}{{renderPartial .Name .}}{{end}}{{end}}`)}
			catalog := []handlers.Descriptor{{Name: "site", New: func() handlers.Handler { return site }, Requires: []handlers.Requirement{
				{Capability: "fixture.components", Multiple: true, Optional: true},
			}}}
			for _, name := range loaded {
				contribution := &templateContribution{Name: name, Files: []content.TemplatePath{
					file("component.html", `{{define "`+name+`"}}<p>`+name+`:{{.Data}}</p>{{end}}`),
				}}
				catalog = append(catalog, handlers.Descriptor{Name: name, Provides: []string{"fixture.components"}, New: func() handlers.Handler {
					return &componentModule{contribution: contribution}
				}})
			}
			if err := handlers.Setup(t.Context(), app, nil, catalog, nil); err != nil {
				t.Fatal(err)
			}
			for _, source := range sources {
				source.serving.Store(true)
			}
			t.Cleanup(func() {
				for _, source := range sources {
					if got := source.lateReads.Load(); got != 0 {
						t.Errorf("requests reread templates: %d", got)
					}
				}
			})
			for _, mode := range []string{"page", "fragment", "history", "full"} {
				for _, layout := range []string{"", "compact"} {
					t.Run(mode+"/"+layout, func(t *testing.T) {
						t.Parallel()
						value := "<" + strings.Join(loaded, "-") + "/" + mode + "/" + layout + ">"
						body := func(names []string) string {
							var result string
							for _, name := range names {
								result += "<p>" + name + ":" + html.EscapeString(value) + "</p>"
							}
							return result
						}
						query := url.Values{"value": {value}, "layout": {layout}}
						request := func(query url.Values, want string, status int) {
							t.Helper()
							r := httptest.NewRequest(http.MethodGet, "/?"+query.Encode(), nil)
							if mode != "page" {
								r.Header.Set(htmx.HeaderRequest, "true")
							}
							if mode == "history" {
								r.Header.Set(htmx.HeaderHistoryRestoreRequest, "true")
							}
							if mode == "full" {
								r.Header.Set(htmx.HeaderRequestType, "full")
							}
							if status == http.StatusOK && mode != "fragment" {
								if layout == "compact" {
									want = "<article>" + want + "</article>"
								} else {
									want = "<main>" + want + "</main>"
								}
							}
							w := httptest.NewRecorder()
							app.Web.ServeHTTP(w, r)
							if w.Code != status || status == http.StatusOK && w.Body.String() != want {
								t.Fatalf("%s: status=%d body=%q, want %d %q", r.URL, w.Code, w.Body.String(), status, want)
							}
						}
						request(query, body(loaded), http.StatusOK)
						if len(loaded) > 0 {
							last := loaded[len(loaded)-1]
							query["component"] = []string{last}
							request(query, body([]string{last}), http.StatusOK)
							if len(loaded) == 2 {
								query["component"] = []string{last, loaded[0]}
								request(query, body(query["component"]), http.StatusOK)
							}
							query.Set("partial", last)
							request(query, body([]string{last}), http.StatusOK)
							query.Del("partial")
						}
						query.Set("component", "unloaded")
						request(query, "", http.StatusNotFound)
					})
				}
			}
		})
	}
}

func TestModuleCompositionRejectsInvalidTemplates(t *testing.T) {
	for _, source := range []string{`{{if}}`, `{{define "wrong-name"}}not the supplied component{{end}}`} {
		app, err := server.NewApplicationServer(config.Config{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { app.Shutdown() })
		files := fstest.MapFS{
			"page.html": {Data: []byte(`{{renderPartial .PartialName .}}{{define "fixture.composition"}}{{range .Data}}{{renderPartial .Name .}}{{end}}{{end}}{{define "fixture.compact"}}{{renderPartial .PartialName .}}{{end}}`)},
			"bad.html":  {Data: []byte(source)},
		}
		site := &compositionModule{files: content.TemplatePath{FS: files, Path: "page.html"}}
		catalog := []handlers.Descriptor{
			{Name: "site", New: func() handlers.Handler { return site }, Requires: []handlers.Requirement{{Capability: "fixture.components", Multiple: true}}},
			{Name: "bad", Provides: []string{"fixture.components"}, New: func() handlers.Handler {
				return &componentModule{contribution: &templateContribution{Name: "bad", Files: []content.TemplatePath{{FS: files, Path: "bad.html"}}}}
			}},
		}
		err = handlers.Setup(t.Context(), app, nil, catalog, nil)
		if err == nil || site.routesBound {
			t.Fatalf("invalid composition reached routes: error=%v routes=%t", err, site.routesBound)
		}
		if source == `{{if}}` && !errors.Is(err, content.ErrParsingTemplates) {
			t.Fatalf("lost parsing error: %v", err)
		}
	}
}
