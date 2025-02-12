// site.go defines the site module
package module_site

import (
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

type (
	// SiteModule contains all of the data required to implement the site module
	SiteModule struct {
		Title    string
		Database *sqlx.DB

		contentManager *content.ContentManagerService
		sessionManager *session.SessionManager
	}
)

const (
	module             = "module_site"
	pageLayoutTemplate = "modules/site/templates/html/layouts/site.html"
	homeContent        = "modules/site/templates/html/index.html"
)

// init registers an instance of SiteModule with the application handlers. init runs
// when the module_site
func init() {
	handlers.Register(new(SiteModule))
}

// Init takes care of initializing the specified SiteModule instance
func (m *SiteModule) Init(s *server.ApplicationServer) error {
	m.Title = s.Config.App.Title

	m.Database = s.Database
	m.contentManager = content.NewContentManager()
	m.contentManager.AddPageLayoutTemplate(content.Template(pageLayoutTemplate))

	m.sessionManager = s.Session

	return nil
}

// Error notifies the requestor of the specified error message and logs the provided error
// if the logError parameter is true
func (m *SiteModule) Error(w http.ResponseWriter, r *http.Request, errorMessage string, err error, logError bool) {
	if logError {
		logger.LogRequestError(r, errorMessage, err)
	}

	http.Error(w, errorMessage, http.StatusInternalServerError)
}
