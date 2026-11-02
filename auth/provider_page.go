package auth

import (
	"html/template"

	"github.com/trentnix/hyperserver/pkg/components/content"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

// ProviderPage separates the shared page presentation from provider-owned controls.
// Workflow is login, register, or empty for a completion message. Controls and
// Alternatives must contain reviewed provider markup, never unescaped user input.
type ProviderPage struct {
	Workflow, Title, Message string
	Controls                 []template.HTML
	Alternatives             []template.HTML
}

// PrepareProviderView prepares the shared external-provider page and optional
// provider-owned partials. Applications can override auth.provider.introduction
// without naming a provider or copying its branded controls.
func PrepareProviderView(cm *contentservice.ContentManagerService, additional ...content.TemplatePath) (*content.View, error) {
	paths := append([]content.TemplatePath{{FS: authModuleTemplatesFS, Path: "templates/html/pages/provider.html"}}, additional...)
	return content.PrepareView(cm, "auth.page.provider", paths...)
}
