// Package content combines page data and templates into full-page or HTMX responses.
// Content can use templates prepared at startup or parse explicitly supplied files.
// Template output is buffered before delivery.
package content

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/components/messages"
	"github.com/trentnix/hyperserver/pkg/components/types"
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
	content_services "github.com/trentnix/hyperserver/pkg/services/content"
)

type (
	// TemplatePath identifies a template file passed to the renderer.
	TemplatePath = types.TemplatePath

	// Content holds template inputs and HTTP response metadata for a page or fragment.
	Content struct {
		// Site name
		Site string

		// Title of the content, if any
		Title string

		// PartialName is the named template fragment to render from the layout.
		PartialName string

		// Templates holds reusable page and fragment templates prepared at startup.
		// When set, request-specific Layouts, Contents, and Components must be empty.
		Templates *Templates

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

		// RedirectURL selects a local redirect instead of rendering templates.
		RedirectURL string

		// OutOfBand names templates appended to HTMX responses, in order.
		// Each template must supply its own hx-swap-oob markup.
		OutOfBand []string

		HTMX struct {
			// Request contains the information provided by HTMX about the current request
			Request htmx.Request

			// Response contains values to pass back to HTMX
			Response *htmx.Response
		}

		// Data contains the data that should be rendered to any available templates
		Data any

		// LayoutData holds request-specific layout inputs, separate from page Data.
		LayoutData any

		// ContentManager contains layouts, content, and components that may be used
		// across the application and need to be combined when the Content is rendered
		ContentManager *content_services.ContentManagerService

		// Messages that should be written/rendered
		Messages []messages.Message

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
	c.PartialName = ""
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

// NewManagedContent creates request-specific content with defaults from cm.
// It returns nil if cm is nil.
func NewManagedContent(r *http.Request, cm *content_services.ContentManagerService) *Content {
	if cm == nil {
		return nil
	}

	c := NewContent(r)
	c.ContentManager = cm
	c.Site = c.ContentManager.AppName
	c.Title = c.ContentManager.AppTitle
	if r != nil && cm.BuildLayoutData != nil {
		c.LayoutData = cm.BuildLayoutData(r)
	}

	return c
}

// IsHtmx reports whether the response uses HTMX fragments.
// History restoration needs a full page, even when HX-Request is true.
func (c *Content) IsHtmx() bool {
	return c.HTMX.Request.Enabled && !c.HTMX.Request.HistoryRestore
}

// AddLayout appends layout paths relative to files, in order.
// With no names, it leaves the paths unchanged.
func (c *Content) AddLayout(files fs.FS, names ...string) {
	for _, name := range names {
		c.Layouts = append(c.Layouts, TemplatePath{FS: files, Path: name})
	}
}

// AddContent appends content paths relative to files, in order.
// With no names, it leaves the paths unchanged.
func (c *Content) AddContent(files fs.FS, names ...string) {
	for _, name := range names {
		c.Contents = append(c.Contents, TemplatePath{FS: files, Path: name})
	}
}

// AddComponent appends component paths relative to files, in order.
// With no names, it leaves the paths unchanged.
func (c *Content) AddComponent(files fs.FS, names ...string) {
	for _, name := range names {
		c.Components = append(c.Components, TemplatePath{FS: files, Path: name})
	}
}

// AddLogMessage adds the provided string to the Content's LogMessages
func (c *Content) AddLogMessage(message string) {
	c.LogMessages = append(c.LogMessages, message)
}

// AddLogMessages adds the provided string slice to the Content's LogMessages
func (c *Content) AddLogMessages(messages ...string) {
	c.LogMessages = append(c.LogMessages, messages...)
}

// templatePaths combines manager and request templates in rendering order.
func (c *Content) templatePaths() []TemplatePath {
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
	return templates
}

// render produces escaped HTML without writing an HTTP response or loading notifications.
func (c *Content) render() (*bytes.Buffer, error) {
	var parsed *parsedTemplates
	if c.Templates != nil {
		if len(c.Layouts)+len(c.Contents)+len(c.Components) != 0 {
			return nil, hs_errors.WithCause(ErrRenderingTemplates, fmt.Errorf("prepared templates cannot be combined with request template files"))
		}
		parsed = c.Templates.page
		if c.IsHtmx() {
			parsed = c.Templates.fragment
		}
		if parsed == nil {
			return nil, hs_errors.WithCause(ErrRenderingTemplates, fmt.Errorf("templates have not been prepared"))
		}
	} else {
		var err error
		parsed, err = parseTemplates(c.templatePaths())
		if err != nil {
			return nil, err
		}
	}

	var body bytes.Buffer
	err := parsed.template.ExecuteTemplate(&body, parsed.root, c)
	if err != nil {
		return nil, hs_errors.WithCause(ErrRenderingTemplates, err)
	}
	if c.IsHtmx() {
		for _, name := range c.OutOfBand {
			if err := parsed.template.ExecuteTemplate(&body, name, c); err != nil {
				return nil, hs_errors.WithCause(ErrRenderingTemplates, err)
			}
		}
	}

	return &body, nil
}
