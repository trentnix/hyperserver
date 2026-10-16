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
		renderer := &pageRenderer{contentManager: cm}
		cm.PrepareTemplates = renderer.prepareTemplates
		cm.HandleMessage = renderer.HandleMessage
		cm.HandleError = renderer.HandleError
		cm.HandleNotFound = renderer.NotFound
	})
}

func (m *pageRenderer) prepareTemplates() error {
	errorTemplates, err := content.PrepareTemplates(m.contentManager,
		content.TemplatePath{FS: siteModuleTemplatesFS, Path: errorPageTemplate})
	if err != nil {
		return fmt.Errorf("prepare site error response: %w", err)
	}

	views := make(map[string]*content.View)
	for _, page := range []struct{ file, partial string }{
		{messagePageTemplate, messagePagePartialName},
		{notFoundPageTemplate, notFoundPagePartialName},
	} {
		view, err := content.PrepareView(m.contentManager, page.partial,
			content.TemplatePath{FS: siteModuleTemplatesFS, Path: page.file})
		if err != nil {
			return fmt.Errorf("prepare site response %s: %w", page.partial, err)
		}
		views[page.partial] = view
	}
	m.views, m.errorTemplates = views, errorTemplates
	return nil
}
