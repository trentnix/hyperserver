// Package greeting registers a read-only form and its page and fragment templates.
package greeting

import (
	"context"
	"embed"
	"net/http"
	"strings"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

//go:embed templates/*.html static/*.css
var greetingModuleTemplatesFS embed.FS

func init() {
	handlers.Register(handlers.Descriptor{Name: "greeting", New: func() handlers.Handler { return new(module) }})
}

type module struct {
	contentManager *contentservice.ContentManagerService
	templates      *content.Templates
}

func (m *module) Init(_ context.Context, app *server.ApplicationServer) error {
	m.contentManager = app.ContentManager
	templates, err := content.PrepareTemplates(m.contentManager,
		content.TemplatePath{FS: greetingModuleTemplatesFS, Path: "templates/greeting.html"})
	if err != nil {
		return err
	}
	m.templates = templates
	return nil
}

// ConfigureRendering selects this application's layouts before module setup.
func ConfigureRendering(cm *contentservice.ContentManagerService) error {
	return cm.ApplyDefaults("greeting", func(cm *contentservice.ContentManagerService) {
		cm.RegisterLayouts(contentservice.PageType, greetingModuleTemplatesFS, "templates/page.html")
		cm.RegisterLayouts(contentservice.HtmxType, greetingModuleTemplatesFS, "templates/fragment.html")
	})
}

func (m *module) Routes(routes *routing.Routes) error {
	routes.HandleFunc("GET /{$}", m.greet)
	routes.HandleFunc("GET /assets/app.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, greetingModuleTemplatesFS, "static/app.css")
	})
	return routes.Err()
}

func (m *module) greet(w http.ResponseWriter, r *http.Request) {
	input := &greetingForm{}
	page := content.NewManagedContent(r, m.contentManager)
	page.Templates, page.Data = m.templates, input
	page.PartialName = "greeting.form"
	if r.URL.RawQuery != "" {
		if err := form.ParseAndValidate(r, input); err != nil {
			form.HandleFormError(w, r, page, input, "Unable to read the name", err)
			return
		}
	}
	if err := page.Render(w, r); err != nil {
		m.contentManager.HandleError(w, r, "Unable to display greeting", err, http.StatusInternalServerError)
	}
}

type greetingForm struct {
	form.Form
	Name string
}

func (f *greetingForm) Bind(r *http.Request) error {
	f.Name = strings.TrimSpace(r.Form.Get("name"))
	return nil
}

func (f *greetingForm) Validate() form.FieldErrors {
	if f.Name == "" {
		return form.FieldErrors{"Name": {"Enter your name."}}
	}
	return nil
}
