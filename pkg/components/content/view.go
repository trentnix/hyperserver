package content

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"

	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

// View binds prepared templates and a named partial to application rendering settings.
// Share it between requests. Each call to Content creates request-owned response data.
// Construct views during initialization with PrepareView or Templates.View.
// The zero value cannot render.
type View struct {
	manager   *contentservice.ContentManagerService
	templates Templates
	partial   string
}

// PrepareView prepares page and HTMX templates and checks that partial exists in both.
// Configure cm first and return any error to startup. A nil cm uses only the supplied
// files and default Content settings. Layouts must render the selected PartialName.
func PrepareView(cm *contentservice.ContentManagerService, partial string, contents ...TemplatePath) (*View, error) {
	templates, err := PrepareTemplates(cm, contents...)
	if err != nil {
		return nil, err
	}
	return templates.View(cm, partial)
}

// View selects a named partial without parsing or reading files again. Use it to
// serve several partials from one template set. Supply the same cm used to prepare
// the templates. Its callbacks remain shared and must not change while serving.
func (t *Templates) View(cm *contentservice.ContentManagerService, partial string) (*View, error) {
	if t == nil || t.page == nil || t.fragment == nil {
		return nil, fmt.Errorf("view requires prepared templates")
	}
	if partial == "" {
		return nil, fmt.Errorf("view requires a partial template name")
	}
	for _, variant := range []struct {
		name   string
		parsed *parsedTemplates
	}{
		{"page", t.page},
		{"fragment", t.fragment},
	} {
		if variant.parsed.template.Lookup(partial) == nil {
			return nil, fmt.Errorf("%s partial template not found: %s", variant.name, partial)
		}
	}
	return &View{manager: cm, templates: *t, partial: partial}, nil
}

// Content creates a response using the view's templates and the supplied data.
// Set response-specific fields as needed, then call Content.Render or pass it to
// form handling. Data is not copied. Callers must not share mutable request data.
func (v *View) Content(r *http.Request, data any) *Content {
	var c *Content
	if v.manager == nil {
		c = NewContent(r)
	} else {
		c = NewManagedContent(r, v.manager)
	}
	c.Templates, c.PartialName, c.Data = &v.templates, v.partial, data
	return c
}

// RenderPartial renders only the view's named partial with request-local data.
// It preserves application overrides without rendering a layout, loading
// notifications, or writing an HTTP response. Errors return no partial output.
func (v *View) RenderPartial(r *http.Request, data any) (template.HTML, error) {
	if v == nil || v.templates.page == nil || v.templates.fragment == nil {
		return "", fmt.Errorf("view requires prepared templates")
	}
	c := v.Content(r, data)
	parsed := v.templates.page
	if c.IsHtmx() {
		parsed = v.templates.fragment
	}
	var body bytes.Buffer
	if err := parsed.template.ExecuteTemplate(&body, v.partial, c); err != nil {
		return "", fmt.Errorf("render partial %s: %w", v.partial, err)
	}
	// Only escaped html/template output crosses this trust boundary.
	return template.HTML(body.String()), nil
}
