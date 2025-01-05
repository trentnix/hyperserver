// content.go defines the Content structure which is used as a ViewModel for the templates that
// are rendered to a requestor
package content

import (
	"fmt"
	"net/http"
	"text/template"

	"github.com/trentnix/hyperserver/pkg/services/htmx"
)

// Page defines the various fields that describe a particular site page
type (
	Content struct {
		// Title of the content, if any
		Title string

		// URL of the displayed content - optional
		URL string

		// Layout specifies the root layout used in the page
		LayoutTemplates []Template

		// Contents is a list of the various templates that should be loaded
		ContentTemplates []Template

		// Components contains shared components loaded into a template
		Components Template

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
	}
)

const (
	// default page title and title prefix
	DefaultTitle = "HyperServer"
)

// NewPage extracts Page data from the provided context
func NewContent(r *http.Request) *Content {
	c := Content{}
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

// IsHtmx will let the caller determine whether the specified content is an HTMX response to an HTMX request
func (c *Content) IsHtmx() bool {
	if c.HTMX.Response != nil && c.HTMX.Request.Enabled {
		return true
	}

	return false
}

// AddContents adds the content slice of strings to the Page.Content slice
func (c *Content) AddTemplates(contents ...Template) {
	c.ContentTemplates = append(c.ContentTemplates, contents...)
}

// Render parses the layout and content templates and executes them with the
// specified Content as the view model
func (c *Content) Render(w http.ResponseWriter) error {
	templates := c.LayoutTemplates
	templates = append(templates, c.ContentTemplates...)
	if len(templates) == 0 {
		return fmt.Errorf("No templates have been set")
	}

	tmpl, err := template.ParseFiles(templatesToStrings(c.ContentTemplates)...)
	if err != nil {
		return fmt.Errorf("Error loading template: %w", err)
	}

	// render the template
	err = tmpl.Execute(w, c)
	if err != nil {
		return fmt.Errorf("Error rendering template: %w", err)
	}

	return nil
}

// templatesToStrings takes the specified Template slice and converts it to a string slice
func templatesToStrings(templates []Template) []string {
	strings := make([]string, len(templates))
	for i, t := range templates {
		strings[i] = string(t)
	}

	return strings
}
