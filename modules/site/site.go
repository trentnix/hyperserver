// site.go defines the site module
package module_site

import (
	"net/http"
	"path/filepath"
	"text/template"

	"github.com/trentnix/hyperserver/handlers"
	"github.com/trentnix/hyperserver/server"
)

type (
	// SiteModule contains all of the data required to implement the site module
	SiteModule struct {
		title string
	}
)

// init registers an instance of SiteModule with the application handlers. init runs
// when the module_site
func init() {
	handlers.Register(new(SiteModule))
}

// Init takes care of initializing the specified SiteModule instance
func (m *SiteModule) Init(s *server.ApplicationServer) error {
	m.title = s.Config.App.Title

	return nil
}

// Routes registers routes with the provided router and, along with Init, satisfies
// the Handler interface
func (m *SiteModule) Routes(mux *http.ServeMux) {
	mux.Handle("/", http.HandlerFunc(m.Home))
}

// Home renders the homepage and handles the following routes:
//
//	/
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
