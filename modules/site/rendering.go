package module_site

import (
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

// ConfigureRendering selects the site's shared layouts, navigation, response
// handlers, and redirect URLs. Call during application setup, before overrides
// and module initialization. Module setup calls the installed PrepareTemplates
// callback after overrides. Direct users must call it before invoking handlers.
// This function does not enable notifications or acquire services.
func ConfigureRendering(cm *contentservice.ContentManagerService, authEnabled bool) error {
	return cm.ApplyDefaults("site", func(cm *contentservice.ContentManagerService) {
		cm.RegisterLayouts(contentservice.PageType, siteModuleTemplatesFS, pageLayoutTemplate)
		cm.RegisterLayouts(contentservice.HtmxType, siteModuleTemplatesFS, partialLayoutTemplate)
		cm.RegisterComponents(contentservice.PageType, siteModuleTemplatesFS,
			navComponentTemplate, notificationsComponentTemplate, consoleMessagesComponentTemplate)
		cm.BuildLayoutData = func(r *http.Request) any {
			return struct {
				AuthEnabled   bool
				Authenticated bool
			}{authEnabled, user.GetUserFromContext(r.Context()) != nil}
		}

		cm.HomeURL = homeURL
		cm.AuthURL = authURL
		cm.ErrorURL = errorURL
		cm.NotFoundURL = notFoundURL
		renderer := &pageRenderer{contentManager: cm}
		cm.PrepareTemplates = renderer.prepareTemplates
		cm.HandleMessage = renderer.HandleMessage
		cm.HandleError = renderer.HandleError
		cm.HandleNotFound = renderer.NotFound
	})
}

func (m *pageRenderer) prepareTemplates() error {
	prepared := make(map[string]*content.Templates)
	for _, name := range []string{
		errorPageTemplate,
		messagePageTemplate,
		notFoundPageTemplate,
	} {
		templates, err := content.PrepareTemplates(m.contentManager, content.TemplatePath{FS: siteModuleTemplatesFS, Path: name})
		if err != nil {
			return fmt.Errorf("prepare site response %s: %w", name, err)
		}
		prepared[name] = templates
	}
	m.templates = prepared
	return nil
}
