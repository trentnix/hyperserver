// Package content combines page data and templates into full-page or HTMX responses.
// Templates must be prepared at startup before rendering requests.
// Template output is buffered before delivery.
package content

import (
	"bytes"
	"fmt"
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
		Templates *Templates

		// URL of the displayed content - optional
		URL string

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

		// ContentManager supplies shared response settings and callbacks.
		ContentManager *content_services.ContentManagerService

		// Messages that should be written/rendered
		Messages []messages.Message

		// Notifications that should be written/rendered
		Notifications []messages.Notification

		// System messages that should be written/rendered
		SystemMessages []messages.SystemMessage
	}
)

// NewContent extracts Content data from the provided request
func NewContent(r *http.Request) *Content {
	c := &Content{Site: content_services.DefaultAppName, Title: content_services.DefaultAppTitle, ResponseStatusCode: http.StatusOK}
	if r != nil {
		c.HTMX.Request = htmx.GetRequest(r)
	}
	return c
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
// History restoration and full-document requests need the layout, even when HX-Request is true.
func (c *Content) IsHtmx() bool {
	return c.HTMX.Request.Enabled && !c.HTMX.Request.HistoryRestore && !c.HTMX.Request.FullPage
}

// render produces escaped HTML without writing an HTTP response or loading notifications.
func (c *Content) render() (*bytes.Buffer, error) {
	if c.Templates == nil {
		return nil, hs_errors.WithCause(ErrNoTemplates, fmt.Errorf("No templates have been set"))
	}
	parsed := c.Templates.page
	if c.IsHtmx() {
		parsed = c.Templates.fragment
	}
	if parsed == nil {
		return nil, hs_errors.WithCause(ErrRenderingTemplates, fmt.Errorf("templates have not been prepared"))
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
