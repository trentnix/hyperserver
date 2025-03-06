// content.go defines the Content structure which is used as a ViewModel for the templates that
// are rendered to a requestor
package content

import (
	"fmt"
	"net/http"
	"text/template"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
)

// Page defines the various fields that describe a particular site page
type (
	Content struct {
		// Site name
		Site string

		// Title of the content, if any
		Title string

		// URL of the displayed content - optional
		URL string

		// Layout specifies the root layout used in the page
		LayoutTemplates []Template

		// Contents is a list of the various templates that should be loaded
		ContentTemplates []Template

		// Components contains shared components loaded into a template
		ComponentTemplates []Template

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

		// complex data that needs to be rendered on a given page
		Data any

		ContentManager *ContentManagerService

		Messages []ContentMessage
	}

	ContentMessage struct {
		Message     string
		MessageType string
	}
)

const (
	// default page title and title prefix
	DefaultSite  = "HyperServer"
	DefaultTitle = "HyperServer"

	messageTypeDefault = "default"
	messageTypeSuccess = "success"
	messageTypeError   = "error"
)

// NewContent extracts Content data from the provided request
func NewContent(r *http.Request) *Content {
	c := Content{}
	c.Site = DefaultSite
	c.Title = DefaultTitle
	c.ResponseStatusCode = 200

	if r != nil {
		// retrieve the user information from the session data
		c.HTMX.Request = htmx.GetRequest(r)
		if c.HTMX.Request.Enabled {
			c.LayoutTemplates = []Template{}
		}
	}

	return &c
}

// NewManagedContent extracts Content data from the provided request and also sets
// the Content's ContentManagerService
func NewManagedContent(r *http.Request) *Content {
	c := NewContent(r)
	c.ContentManager = GetContentManager()

	return c
}

// IsHtmx will let the caller determine whether the specified content is an HTMX response to an HTMX request
func (c *Content) IsHtmx() bool {
	return c.HTMX.Request.Enabled
}

// AddLayoutTemplate adds the content Template to the Content's LayoutTemplates
func (c *Content) AddLayoutTemplate(content Template) {
	c.LayoutTemplates = append(c.LayoutTemplates, content)
}

// AddLayoutTemplates adds the contents Templates to the Content's LayoutTemplates
func (c *Content) AddLayoutTemplates(contents ...Template) {
	c.LayoutTemplates = append(c.LayoutTemplates, contents...)
}

// AddContentTemplate adds the content Template to the Content's ContentTemplates
func (c *Content) AddContentTemplate(content Template) {
	c.ContentTemplates = append(c.ContentTemplates, content)
}

// AddContentTemplates adds the contents Templates to the Content's ContentTemplates
func (c *Content) AddContentTemplates(contents ...Template) {
	c.ContentTemplates = append(c.ContentTemplates, contents...)
}

// AddComponentTemplate adds the content Template to the Content's ComponentTemplates
func (c *Content) AddComponentTemplate(content Template) {
	c.ComponentTemplates = append(c.ComponentTemplates, content)
}

// AddComponentTemplates adds the contents Templates to the Content's ComponentTemplates
func (c *Content) AddComponentTemplates(contents ...Template) {
	c.ComponentTemplates = append(c.ComponentTemplates, contents...)
}

// Render parses the layout and content templates and executes them with the
// specified Content as the view model
func (c *Content) Render(w http.ResponseWriter) error {
	if c.ContentManager != nil {
		contentType := PageType
		if c.IsHtmx() {
			// partial page
			contentType = HtmxType
		}

		// load the templates from the ContentManagerService *before* any templates that might be
		// on the specified Content instance
		c.LayoutTemplates = append(c.ContentManager.LayoutTemplates[contentType], c.LayoutTemplates...)
		c.ContentTemplates = append(c.ContentManager.ContentTemplates[contentType], c.ContentTemplates...)
		c.ComponentTemplates = append(c.ContentManager.ComponentTemplates[contentType], c.ComponentTemplates...)
	}

	// create a list of templates in order from the layout templates to the component templates
	var templates []Template
	templates = append(templates, c.LayoutTemplates...)
	templates = append(templates, c.ContentTemplates...)
	templates = append(templates, c.ComponentTemplates...)
	if len(templates) == 0 {
		return NewErrNoTemplates(fmt.Errorf("No templates have been set"))
	}

	// parse the templates in the order specified
	tmpl, err := template.ParseFiles(templatesToStrings(templates)...)
	if err != nil {
		return NewErrParsingTemplates(err)
	}

	// render the template
	err = tmpl.Execute(w, c)
	if err != nil {
		return NewErrRenderingTemplates(err)
	}

	return nil
}

// addMessage adds a new ContentMessage to the Messages slice
func (c *Content) addMessage(message string, messageType string) {
	c.Messages = append(c.Messages, ContentMessage{Message: message, MessageType: messageType})
}

// AddMessage adds simple message to the Messages slice
func (c *Content) AddMessage(message string) {
	c.addMessage(message, messageTypeDefault)
}

// AddErrorMessage adds an error message to the Messages slice
func (c *Content) AddErrorMessage(message string) {
	c.addMessage(message, messageTypeError)
}

// AddSuccessMessage adds an success message to the Messages slice
func (c *Content) AddSuccessMessage(message string) {
	c.addMessage(message, messageTypeSuccess)
}

// templatesToStrings takes the specified Template slice and converts it to a string slice
func templatesToStrings(templates []Template) []string {
	strings := make([]string, len(templates))
	for i, t := range templates {
		strings[i] = string(t)
	}

	return strings
}

// IsDefault returns true if the specified ContentMessage is a default message
func (c *ContentMessage) IsDefault() bool {
	return c.MessageType == messageTypeDefault
}

// IsSuccess returns true if the specified ContentMessage is a success message
func (c *ContentMessage) IsSuccess() bool {
	return c.MessageType == messageTypeSuccess
}

// IsError returns true if the specified ContentMessage is an error message
func (c *ContentMessage) IsError() bool {
	return c.MessageType == messageTypeError
}
