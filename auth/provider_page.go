package auth

import (
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
)

// ProviderPage separates the shared page presentation from provider-owned controls.
// Workflow is login, register, or empty for a completion message. Controls and
// Form must contain reviewed provider markup, never unescaped user input.
type ProviderPage struct {
	Workflow, Title, Message string
	Controls                 []template.HTML
	Form                     template.HTML
	Messages                 []messages.Message
}

// PrepareProviderView prepares the shared external-provider page and optional
// provider-owned partials. Applications can override auth.provider.introduction
// without naming a provider or copying its branded controls.
func PrepareProviderView(cm *contentservice.ContentManagerService, additional ...content.TemplatePath) (*content.View, error) {
	paths := append([]content.TemplatePath{{FS: authModuleTemplatesFS, Path: "templates/html/pages/provider.html"}}, additional...)
	return content.PrepareView(cm, "auth.page.provider", paths...)
}

func (a *AuthManager) renderAuthPage(w http.ResponseWriter, r *http.Request, page ProviderPage, file, partial string) {
	var err error
	page.Messages, err = flash.GetMessages(w, r, flash.AuthMessages)
	if err != nil {
		a.contentManager.HandleError(w, r, "Unable to load authentication messages.", err, http.StatusInternalServerError)
		return
	}
	c := content.NewManagedContent(r, a.contentManager)
	c.Title = page.Title
	c.PartialName, c.Templates, c.Data = partial, a.templates[file], page
	if err := c.Render(w, r); err != nil {
		c.HandleRenderError(w, r, "Unable to display the authentication page.", err)
	}
}
