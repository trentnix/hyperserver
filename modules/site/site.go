// site.go defines the site module
package module_site

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/trentnix/hyperserver/components/content"
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
	// media paths
	cssPath := filepath.Join("modules", "site", "templates", "html", "css")
	jsPath := filepath.Join("modules", "site", "templates", "html", "js")
	imgPath := filepath.Join("modules", "site", "templates", "html", "img")

	// serve media
	mux.Handle("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir(cssPath)))) // Serve .css files
	mux.Handle("/js/", http.StripPrefix("/js/", http.FileServer(http.Dir(jsPath))))    // Serve .js files
	mux.Handle("/img/", http.StripPrefix("/img/", http.FileServer(http.Dir(imgPath)))) // Serve image files

	// serve files
	mux.HandleFunc("/favicon.ico", m.ServeFavicon)

	// serve pages
	mux.Handle("/", http.HandlerFunc(m.Home))

	// contact
	mux.Handle("GET /contact", http.HandlerFunc(m.GetContact))
	mux.Handle("POST /contact", http.HandlerFunc(m.Contact))
}

// Home renders the homepage and handles the following routes:
func (m *SiteModule) Home(w http.ResponseWriter, r *http.Request) {
	pageTemplate := filepath.Join("modules", "site", "templates", "html", "index.html")

	homepage := content.NewContent(r)
	homepage.AddTemplates(content.Template(pageTemplate))
	err := homepage.Render(w)
	if err != nil {
		http.Error(w, fmt.Sprintf("There was an error rendering the specified content: %s", err.Error()), http.StatusInternalServerError)
	}
}

// ServeFavicon serves the favicon resource to a requestor
func (m *SiteModule) ServeFavicon(w http.ResponseWriter, r *http.Request) {
	faviconPath := filepath.Join("modules", "site", "templates", "html", "img", "favicon.ico")
	http.ServeFile(w, r, faviconPath)
}
