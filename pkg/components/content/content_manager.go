// content-manager.go contains the definition of the ContentManagerService, which manages templates
// used to render data to the requestor
package content

import (
	"net/http"
	"sync"
)

type (
	ContentManagerService struct {
		// layout templates will be the lattice on which content is rendered
		LayoutTemplates map[string][]Template
		// content should contain the data rendered in a layout (or data sent to the server without a layout)
		ContentTemplates map[string][]Template
		// components should be the helper templates used to manage layouts and content
		ComponentTemplates map[string][]Template

		// url of the default home page - used for redirects
		HomeURL string
		// url of the default authentication portal page - used for redirects
		AuthURL string

		// HomeHandler lets a caller access the "home" handler
		HomeHandler http.HandlerFunc
		// ErrorHandler lets the caller access the "error" handler
		ErrorHandler func(w http.ResponseWriter, r *http.Request, code int, message string)
	}
)

const (
	// PageType should be used when an entire page is rendered
	PageType = "page"
	// HtmxType should be used when an HTMX response is required
	HtmxType = "htmx"

	homeDefault = "/"
	authDefault = "/login"
)

var (
	// singleton instance of a ContentManagerService
	contentManager *ContentManagerService
	// used to manage the singleton
	once sync.Once
)

// GetContentManager returns the global ContentManagerService singleton
func GetContentManager() *ContentManagerService {
	once.Do(func() {
		contentManager = NewContentManager()
	})

	return contentManager
}

// NewContentManager returns a (non-Singleton) instance of a ContentManagerService
func NewContentManager() *ContentManagerService {
	contentManager := ContentManagerService{}
	contentManager.LayoutTemplates = make(map[string][]Template)
	contentManager.ContentTemplates = make(map[string][]Template)
	contentManager.ComponentTemplates = make(map[string][]Template)

	contentManager.ErrorHandler = func(w http.ResponseWriter, r *http.Request, code int, message string) {
		http.Error(w, "An unspecified error occurred", http.StatusInternalServerError)
	}

	contentManager.HomeURL = homeDefault
	contentManager.HomeHandler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Welcome to HyperServer!"))
	}

	contentManager.AuthURL = authDefault

	return &contentManager
}

// RegisterLayoutTemplates overwrites the layout templates stored in the specified content manager service
func (c *ContentManagerService) RegisterLayoutTemplates(contentType string, templates []Template) {
	c.LayoutTemplates[contentType] = templates
}

// RegisterContentTemplates overwrites the content templates stored in the specified content manager service
func (c *ContentManagerService) RegisterContentTemplates(contentType string, templates []Template) {
	c.ContentTemplates[contentType] = templates
}

// RegisterComponentTemplates overwrites the component templates stored in the specified content manager service
func (c *ContentManagerService) RegisterComponentTemplates(contentType string, templates []Template) {
	c.ComponentTemplates[contentType] = templates
}

// AddLayoutTemplate adds a layout template to the LayoutTemplates slice for the specified contentType
func (c *ContentManagerService) AddLayoutTemplate(contentType string, template Template) {
	c.LayoutTemplates[contentType] = append(c.LayoutTemplates[contentType], template)
}

// AddLayoutTemplates adds layout templates to the LayoutTemplates slice for the specified contentType
func (c *ContentManagerService) AddLayoutTemplates(contentType string, templates []Template) {
	c.LayoutTemplates[contentType] = append(c.LayoutTemplates[contentType], templates...)
}

// AddContentTemplate adds a content template to the ContentTemplates slice for the specified contentType
func (c *ContentManagerService) AddContentTemplate(contentType string, template Template) {
	c.ContentTemplates[contentType] = append(c.ContentTemplates[contentType], template)
}

// AddContentTemplates adds content templates to the ContentTemplates slice for the specified contentType
func (c *ContentManagerService) AddContentTemplates(contentType string, templates []Template) {
	c.ContentTemplates[contentType] = append(c.ContentTemplates[contentType], templates...)
}

// AddComponentTemplate adds a component template to the ComponentTemplates slice for the specified contentType
func (c *ContentManagerService) AddComponentTemplate(contentType string, template Template) {
	c.ComponentTemplates[contentType] = append(c.ComponentTemplates[contentType], template)
}

// AddComponentTemplates adds component templates to the ComponentTemplates slice for the specified contentType
func (c *ContentManagerService) AddComponentTemplates(contentType string, templates []Template) {
	c.ComponentTemplates[contentType] = append(c.ComponentTemplates[contentType], templates...)
}

// AddPageLayoutTemplate adds a layout template for the PageType content type
func (c *ContentManagerService) AddPageLayoutTemplate(template Template) {
	c.AddLayoutTemplate(PageType, template)
}

// AddPageLayoutTemplates adds layout templates for the PageType content type
func (c *ContentManagerService) AddPageLayoutTemplates(templates []Template) {
	c.AddLayoutTemplates(PageType, templates)
}

// AddHtmxLayoutTemplate adds a layout template for the HtmxType content type
func (c *ContentManagerService) AddHtmxLayoutTemplate(template Template) {
	c.AddLayoutTemplate(HtmxType, template)
}

// AddHtmxLayoutTemplate adds layout templates for the HtmxType content type
func (c *ContentManagerService) AddHtmxLayoutTemplates(templates []Template) {
	c.AddLayoutTemplates(HtmxType, templates)
}

// AddPageContentTemplate adds a content template for the PageType content type
func (c *ContentManagerService) AddPageContentTemplate(template Template) {
	c.AddContentTemplate(PageType, template)
}

// AddPageContentTemplates adds content templates for the PageType content type
func (c *ContentManagerService) AddPageContentTemplates(templates []Template) {
	c.AddContentTemplates(PageType, templates)
}

// AddHtmxContentTemplate adds a content template for the HtmxType content type
func (c *ContentManagerService) AddHtmxContentTemplate(template Template) {
	c.AddContentTemplate(HtmxType, template)
}

// AddHtmxContentTemplates adds content templates for the HtmxType content type
func (c *ContentManagerService) AddHtmxContentTemplates(templates []Template) {
	c.AddContentTemplates(HtmxType, templates)
}

// AddPageComponentTemplate adds a component template for the PageType content type
func (c *ContentManagerService) AddPageComponentTemplate(template Template) {
	c.AddComponentTemplate(PageType, template)
}

// AddPageComponentTemplates adds component templates for the PageType content type
func (c *ContentManagerService) AddPageComponentTemplates(templates []Template) {
	c.AddComponentTemplates(PageType, templates)
}

// AddHtmxComponentTemplate adds component templates for the HtmxType content type
func (c *ContentManagerService) AddHtmxComponentTemplate(template Template) {
	c.AddComponentTemplate(HtmxType, template)
}

// AddHtmxComponentTemplates adds component templates for the HtmxType content type
func (c *ContentManagerService) AddHtmxComponentTemplates(templates []Template) {
	c.AddComponentTemplates(HtmxType, templates)
}

// isReservedContentType determines whether the specified content type is a content type
// that is explicitly reserved for application use
func (c *ContentManagerService) isReservedContentType(contentType string) bool {
	switch contentType {
	case PageType:
		return true
	case HtmxType:
		return true
	}

	return false
}

// RenderHome provides a single function to render the default home page registered
// with the application's content manager
func RenderHome(w http.ResponseWriter, r *http.Request) {
	contentManager := GetContentManager()
	contentManager.HomeHandler(w, r)
}

// RenderError provides a single function to render the default error page registered
// with the application's content manager
func RenderError(w http.ResponseWriter, r *http.Request, code int, message string) {
	contentManager := GetContentManager()
	contentManager.ErrorHandler(w, r, code, message)
}

// HTMXRedirect provides an HTMX response to tell the client to redirect to the prodivided URL
func HTMXRedirect(w http.ResponseWriter, redirectURL string) {
	w.Header().Set("HX-Redirect", redirectURL)
}
