// Package pages provides the stateless greeting application's self-registering module.
package pages

import (
	"context"
	"embed"
	"fmt"
	"net/http"
	"strings"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

//go:embed templates
var helloModuleTemplatesFS embed.FS

func init() {
	handlers.Register(Module())
}

// Module describes the greeting pages without acquiring resources.
func Module() handlers.Descriptor {
	return handlers.Descriptor{Name: "hello", New: func() handlers.Handler { return new(pages) }}
}

type pages struct {
	contentManager *contentservice.ContentManagerService
}

func (p *pages) Init(_ context.Context, app *server.ApplicationServer) error {
	if app == nil || app.ContentManager == nil {
		return fmt.Errorf("hello requires a content manager")
	}
	p.contentManager = app.ContentManager
	return nil
}

// ConfigureRendering selects the greeting application's shared layouts.
// Call before application overrides and module initialization.
func ConfigureRendering(cm *contentservice.ContentManagerService) error {
	return cm.ApplyDefaults("hello", func(cm *contentservice.ContentManagerService) {
		cm.RegisterLayouts(contentservice.PageType, helloModuleTemplatesFS, "templates/page.html")
		cm.RegisterLayouts(contentservice.HtmxType, helloModuleTemplatesFS, "templates/fragment.html")
	})
}

func (p *pages) Routes(routes *routing.Routes) error {
	routes.HandleFunc("GET /{$}", p.greet)
	return routes.Err()
}

func (p *pages) greet(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "world"
	}
	page := content.NewManagedContent(r, p.contentManager)
	page.Data = name
	page.AddContent(helloModuleTemplatesFS, "templates/greeting.html")
	if err := page.Render(w, r); err != nil {
		p.contentManager.HandleError(w, r, "Unable to display greeting", err, http.StatusInternalServerError)
	}
}
