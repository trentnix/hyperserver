// content.go defines the Content structure which is used as a ViewModel for the templates that
// are rendered to a requestor
package content

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/components/types"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
)

// Page defines the various fields that describe a particular site page
type (
	TemplatePath = types.TemplatePath

	Content struct {
		// Site name
		Site string

		// Title of the content, if any
		Title string

		// URL of the displayed content - optional
		URL string

		// Layouts specifies the root layout used in the page
		Layouts []TemplatePath

		// Contents is a list of the various templates that should be loaded
		Contents []TemplatePath

		// Components contains shared components loaded into a template
		Components []TemplatePath

		// Headers stores a list of HTTP headers and values to be set on the response
		Headers map[string]string

		// ResponseStatusCode stores the HTTP status code that will be returned
		ResponseStatusCode int

		HTMX struct {
			// Request contains the information provided by HTMX about the current request
			Request htmx.Request

			// Response contains values to pass back to HTMX
			Response *htmx.Response
		}

		// Data contains the data that should be rendered to any available templates
		Data any

		// ContentManager might contain layouts, content, and components that are used
		// across the application and need to be combined when the Content is rendered
		ContentManager *content_services.ContentManagerService

		// Messages that should be written/rendered
		Messages []messages.ContentMessage

		// Notifications that should be written/rendered
		Notifications []messages.Notification

		// System messages that should be written/rendered
		SystemMessages []messages.SystemMessage

		// LogMessages should be written to a client log or console
		LogMessages []string
	}
)

// NewContent extracts Content data from the provided request
func NewContent(r *http.Request) *Content {
	c := Content{}
	c.Site = content_services.DefaultAppName
	c.Title = content_services.DefaultAppTitle
	c.ResponseStatusCode = http.StatusOK

	if r != nil {
		// retrieve the user information from the session data
		c.HTMX.Request = htmx.GetRequest(r)
		if c.HTMX.Request.Enabled {
			// clear the layouts - an HTMX response should only render content and components
			c.Layouts = []TemplatePath{}
		}
	}

	return &c
}

// NewManagedContent extracts Content data from the provided request and also sets
// the Content's ContentManagerService
func NewManagedContent(r *http.Request, cm *content_services.ContentManagerService) *Content {
	if cm == nil {
		return nil
	}

	c := NewContent(r)
	c.ContentManager = cm
	c.Site = c.ContentManager.AppName
	c.Title = c.ContentManager.AppTitle

	return c
}

// IsHtmx will let the caller determine whether the specified content is an HTMX response to an HTMX request
func (c *Content) IsHtmx() bool {
	return c.HTMX.Request.Enabled
}

// AddLayout adds the content Template to the Content's LayoutTemplates
func (c *Content) AddLayout(content TemplatePath) {
	c.Layouts = append(c.Layouts, content)
}

// AddLayouts adds the contents Templates to the Content's LayoutTemplates
func (c *Content) AddLayouts(contents ...TemplatePath) {
	c.Layouts = append(c.Layouts, contents...)
}

// AddContent adds the content Template to the Content's ContentTemplates
func (c *Content) AddContent(content TemplatePath) {
	c.Contents = append(c.Contents, content)
}

// AddContents adds the contents Templates to the Content's ContentTemplates
func (c *Content) AddContents(contents ...TemplatePath) {
	c.Contents = append(c.Contents, contents...)
}

// AddComponent adds the content Template to the Content's ComponentTemplates
func (c *Content) AddComponent(content TemplatePath) {
	c.Components = append(c.Components, content)
}

// AddComponents adds the contents Templates to the Content's ComponentTemplates
func (c *Content) AddComponents(contents ...TemplatePath) {
	c.Components = append(c.Components, contents...)
}

// AddLogMessage adds the provided string to the Content's LogMessages
func (c *Content) AddLogMessage(message string) {
	c.LogMessages = append(c.LogMessages, message)
}

// AddLogMessages adds the provided string slice to the Content's LogMessages
func (c *Content) AddLogMessages(messages ...string) {
	c.LogMessages = append(c.LogMessages, messages...)
}

// Render parses the layout and content templates and executes them with the specified Content as the
// view model. If a ContentManager is specified, its templates will be prepended to the Content
// instance templates before they are merged and rendered.
func (c *Content) Render(w http.ResponseWriter, r *http.Request) error {
	var managerLayouts, managerContents, managerComponents []TemplatePath
	if c.ContentManager != nil {
		contentType := content_services.PageType
		if c.IsHtmx() {
			// partial page
			contentType = content_services.HtmxType
		}

		// load the templates from the ContentManagerService *before* any templates that might be
		// on the specified Content instance
		managerLayouts = c.ContentManager.Layouts[contentType]
		managerContents = c.ContentManager.Contents[contentType]
		managerComponents = c.ContentManager.Components[contentType]
	}

	// merge them into local slices so we don't mutate c.*
	allLayouts := append([]TemplatePath{}, managerLayouts...)
	allLayouts = append(allLayouts, c.Layouts...)

	allContents := append([]TemplatePath{}, managerContents...)
	allContents = append(allContents, c.Contents...)

	allComponents := append([]TemplatePath{}, managerComponents...)
	allComponents = append(allComponents, c.Components...)

	// create a list of templates in order from the layout templates to the component templates
	var templates []TemplatePath
	templates = append(templates, allLayouts...)
	templates = append(templates, allContents...)
	templates = append(templates, allComponents...)
	if len(templates) == 0 {
		return NewErrNoTemplates(fmt.Errorf("No templates have been set"))
	}

	// set the headers
	for k, v := range c.Headers {
		w.Header().Set(k, v)
	}

	// parse the templates in the order specified
	tmpl, err := template.ParseFiles(templatesToStrings(templates)...)
	if err != nil {
		return NewErrParsingTemplates(err)
	}

	if c.ResponseStatusCode != 0 && c.ResponseStatusCode != 200 {
		w.WriteHeader(c.ResponseStatusCode)
	}

	// render the template
	err = tmpl.Execute(w, c)
	if err != nil {
		return NewErrRenderingTemplates(err)
	}

	return nil
}

// templatesToStrings takes the specified Template slice and converts it to a string slice
func templatesToStrings(templates []TemplatePath) []string {
	paths := make([]string, len(templates))
	for i, t := range templates {
		paths[i] = string(t)
	}

	return paths
}
