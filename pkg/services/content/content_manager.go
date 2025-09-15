// content_manager.go contains the definition of the ContentManagerService, which manages templates
// used to render data to the requestor
package content

import (
	"net/http"
	"strconv"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/types"
)

type (
	TemplatePath = types.TemplatePath

	ContentManagerService struct {
		// Layouts templates will host content and components
		Layouts map[string][]TemplatePath
		// Contents should contain the data rendered in a layout (or data sent to the server without a layout)
		Contents map[string][]TemplatePath
		// Components should be the helper templates used to manage layouts and content
		Components map[string][]TemplatePath

		// url of the default home page - used for redirects
		HomeURL string
		// url of the default authentication portal page - used for redirects
		AuthURL string
		// message URL
		MessageURL string
		// error URL
		ErrorURL string
		// url for 404 errors
		NotFoundURL string

		// HandleMessage lets the caller respond with the specified message
		HandleMessage func(w http.ResponseWriter, r *http.Request, message string)
		// HandleError lets the caller access the "error" handler
		HandleError func(w http.ResponseWriter, r *http.Request, message string, err error, httpStatus int)
		// HandleNotFound
		HandleNotFound http.HandlerFunc

		// name of the application
		AppName string
		// default HTML page title
		AppTitle string

		// host name
		Host string
		// port the application handlers are listening on
		Port string

		// specifies whether notifications should be rendered when rendering content
		RenderNotifications bool
	}
)

const (
	// PageType should be used when an entire page is rendered
	PageType = "page"
	// HtmxType should be used when an HTMX response is required
	HtmxType = "htmx"

	DefaultAppName  = "HyperServer"
	DefaultAppTitle = "HyperServer"

	HomeDefault = "/"

	defaultHost = "localhost"
	defaultPort = "80"
)

// NewContentManager returns a (non-Singleton) instance of a ContentManagerService
func NewContentManager() *ContentManagerService {
	cm := ContentManagerService{}
	cm.Layouts = make(map[string][]TemplatePath)
	cm.Contents = make(map[string][]TemplatePath)
	cm.Components = make(map[string][]TemplatePath)

	cm.HomeURL = HomeDefault

	cm.AppName = DefaultAppName
	cm.AppTitle = DefaultAppTitle

	cm.Host = defaultHost
	cm.Port = defaultPort

	// default value
	cm.RenderNotifications = false

	return &cm
}

// Configure does any configuration on the ContentServiceManager that was loaded from
// the application configuration
func (c *ContentManagerService) Configure(cfg *config.Config) {
	if cfg.App.Name != "" {
		c.AppName = cfg.App.Name
	}

	if cfg.HTTP.Hostname != "" {
		c.Host = cfg.HTTP.Hostname
	}

	c.Port = strconv.Itoa(int(cfg.HTTP.Port))

	c.RenderNotifications = cfg.App.RenderNotifications
}

// RegisterLayouts overwrites the layout templates stored in the specified content manager service
func (c *ContentManagerService) RegisterLayouts(contentType string, templates []TemplatePath) {
	c.Layouts[contentType] = templates
}

// RegisterContents overwrites the content templates stored in the specified content manager service
func (c *ContentManagerService) RegisterContents(contentType string, templates []TemplatePath) {
	c.Contents[contentType] = templates
}

// RegisterComponents overwrites the component templates stored in the specified content manager service
func (c *ContentManagerService) RegisterComponents(contentType string, templates []TemplatePath) {
	c.Components[contentType] = templates
}

// AddLayout adds a layout template to the LayoutTemplates slice for the specified contentType
func (c *ContentManagerService) AddLayout(contentType string, template TemplatePath) {
	c.Layouts[contentType] = append(c.Layouts[contentType], template)
}

// AddLayouts adds layout templates to the LayoutTemplates slice for the specified contentType
func (c *ContentManagerService) AddLayouts(contentType string, templates []TemplatePath) {
	c.Layouts[contentType] = append(c.Layouts[contentType], templates...)
}

// AddContent adds a content template to the ContentTemplates slice for the specified contentType
func (c *ContentManagerService) AddContent(contentType string, template TemplatePath) {
	c.Contents[contentType] = append(c.Contents[contentType], template)
}

// AddContents adds content templates to the ContentTemplates slice for the specified contentType
func (c *ContentManagerService) AddContents(contentType string, templates []TemplatePath) {
	c.Contents[contentType] = append(c.Contents[contentType], templates...)
}

// AddComponent adds a component template to the ComponentTemplates slice for the specified contentType
func (c *ContentManagerService) AddComponent(contentType string, template TemplatePath) {
	c.Components[contentType] = append(c.Components[contentType], template)
}

// AddComponents adds component templates to the ComponentTemplates slice for the specified contentType
func (c *ContentManagerService) AddComponents(contentType string, templates []TemplatePath) {
	c.Components[contentType] = append(c.Components[contentType], templates...)
}

// AddPageLayout adds a layout template for the PageType content type
func (c *ContentManagerService) AddPageLayout(template TemplatePath) {
	c.AddLayout(PageType, template)
}

// AddPageLayouts adds layout templates for the PageType content type
func (c *ContentManagerService) AddPageLayouts(templates []TemplatePath) {
	c.AddLayouts(PageType, templates)
}

// AddHtmxLayout adds a layout template for the HtmxType content type
func (c *ContentManagerService) AddHtmxLayout(template TemplatePath) {
	c.AddLayout(HtmxType, template)
}

// AddHtmxLayoutTemplate adds layout templates for the HtmxType content type
func (c *ContentManagerService) AddHtmxLayouts(templates []TemplatePath) {
	c.AddLayouts(HtmxType, templates)
}

// AddPageContent adds a content template for the PageType content type
func (c *ContentManagerService) AddPageContent(template TemplatePath) {
	c.AddContent(PageType, template)
}

// AddPageContents adds content templates for the PageType content type
func (c *ContentManagerService) AddPageContents(templates []TemplatePath) {
	c.AddContents(PageType, templates)
}

// AddHtmxContent adds a content template for the HtmxType content type
func (c *ContentManagerService) AddHtmxContent(template TemplatePath) {
	c.AddContent(HtmxType, template)
}

// AddHtmxContents adds content templates for the HtmxType content type
func (c *ContentManagerService) AddHtmxContents(templates []TemplatePath) {
	c.AddContents(HtmxType, templates)
}

// AddPageComponent adds a component template for the PageType content type
func (c *ContentManagerService) AddPageComponent(template TemplatePath) {
	c.AddComponent(PageType, template)
}

// AddPageComponents adds component templates for the PageType content type
func (c *ContentManagerService) AddPageComponents(templates []TemplatePath) {
	c.AddComponents(PageType, templates)
}

// AddHtmxComponent adds component templates for the HtmxType content type
func (c *ContentManagerService) AddHtmxComponent(template TemplatePath) {
	c.AddComponent(HtmxType, template)
}

// AddHtmxComponents adds component templates for the HtmxType content type
func (c *ContentManagerService) AddHtmxComponents(templates []TemplatePath) {
	c.AddComponents(HtmxType, templates)
}
