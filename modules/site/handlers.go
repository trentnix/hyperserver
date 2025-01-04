package module_site

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/trentnix/hyperserver/components/content"
)

// Home renders the homepage and handles the following routes:
func (m *SiteModule) Home(w http.ResponseWriter, r *http.Request) {
	homepage := content.NewManagedContent(r, m.contentManager)
	homepage.Site = m.Title
	homepage.Title = "Home"
	homepage.AddContentTemplate(homeContent)
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
