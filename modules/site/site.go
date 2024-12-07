package module_site

import (
	"net/http"
	"path/filepath"
	"text/template"

	"github.com/trentnix/hyperserver/handlers"
	"github.com/trentnix/hyperserver/server"
)

type (
	SiteModule struct {
		title string
	}
)

func init() {
	handlers.Register(new(SiteModule))
}

func (m *SiteModule) Init(s *server.ApplicationServer) error {
	m.title = s.Config.App.Title

	return nil
}

func (m *SiteModule) Routes(mux *http.ServeMux) {
	mux.Handle("/", http.HandlerFunc(m.Home))
}

func (m *SiteModule) Home(w http.ResponseWriter, r *http.Request) {
	templatePath := filepath.Join("modules", "site", "templates", "html", "index.tmpl")

	// Parse the template
	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		http.Error(w, "Error loading template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Define the view model
	viewModel := struct {
		Title string
	}{
		Title: "Welcome to My Site",
	}

	// Render the template
	err = tmpl.Execute(w, viewModel)
	if err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}
