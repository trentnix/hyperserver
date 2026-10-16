package notes

import (
	"context"
	"embed"
	"fmt"
	"net/http"
	"strings"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

//go:embed templates/*.html
var notesModuleTemplatesFS embed.FS

func init() { handlers.Register(Module(nil, nil)) }

// Module supplies this feature's repository without selecting a storage platform.
// protect applies the application's write-access policy. Both arguments are required.
// Application setup owns and closes the repository after module consumers stop.
func Module(repository Repository, protect func(http.Handler) http.Handler) handlers.Descriptor {
	return handlers.Descriptor{Name: "notes", New: func() handlers.Handler { return &module{repository: repository, protect: protect} }}
}

type module struct {
	repository     Repository
	protect        func(http.Handler) http.Handler
	contentManager *contentservice.ContentManagerService
	templates      *content.Templates
}

func (m *module) Init(_ context.Context, app *server.ApplicationServer) error {
	if m.repository == nil {
		return fmt.Errorf("notes requires a repository")
	}
	if m.protect == nil {
		return fmt.Errorf("notes requires a write-access policy")
	}
	m.contentManager = app.ContentManager
	templates, err := content.PrepareTemplates(m.contentManager, content.TemplatePath{FS: notesModuleTemplatesFS, Path: "templates/notes.html"})
	if err != nil {
		return err
	}
	m.templates = templates
	return nil
}

func (m *module) Routes(routes *routing.Routes) error {
	routes.HandleFunc("GET /notes", m.list)
	routes.Handle("POST /notes", m.protect(http.HandlerFunc(m.create)))
	return routes.Err()
}

type noteForm struct {
	form.Form
	Text  string
	Notes []Note
}

func (f *noteForm) Bind(r *http.Request) error {
	f.Text = strings.TrimSpace(r.PostForm.Get("text"))
	return nil
}

func (f *noteForm) Validate() form.FieldErrors {
	if err := ValidateText(f.Text); err != nil {
		return form.FieldErrors{"Text": {err.Error()}}
	}
	return nil
}

func (m *module) page(r *http.Request, input *noteForm) *content.Content {
	page := content.NewManagedContent(r, m.contentManager)
	page.Templates, page.PartialName, page.Data = m.templates, "notes.panel", input
	return page
}

func (m *module) list(w http.ResponseWriter, r *http.Request) {
	input := &noteForm{}
	items, err := m.repository.List(r.Context(), MaxListSize)
	if err != nil {
		m.contentManager.HandleError(w, r, "Unable to load notes", err, http.StatusServiceUnavailable)
		return
	}
	input.Notes = items
	if err := m.page(r, input).Render(w, r); err != nil {
		m.contentManager.HandleError(w, r, "Unable to display notes", err, http.StatusInternalServerError)
	}
}

func (m *module) create(w http.ResponseWriter, r *http.Request) {
	input := &noteForm{}
	page := m.page(r, input)
	if err := form.ParseAndValidate(r, input); err != nil {
		form.HandleFormError(w, r, page, input, "Unable to read the note", err)
		return
	}

	if _, err := Create(r.Context(), m.repository, input.Text); err != nil {
		m.contentManager.HandleError(w, r, "Unable to save note", err, http.StatusServiceUnavailable)
		return
	}

	// Redirect after a successful write instead of risking a second storage failure
	// while rendering success. HTMX receives HX-Redirect, ordinary clients get 303.
	page.RedirectURL = "/notes"
	if err := page.Render(w, r); err != nil {
		m.contentManager.HandleError(w, r, "Unable to redirect to notes", err, http.StatusInternalServerError)
	}
}
