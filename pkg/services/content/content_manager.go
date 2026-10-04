// Package content configures shared templates and response handlers for rendering.
// Configure a ContentManagerService before serving requests. Its maps and callbacks
// are mutable and are not protected against concurrent configuration changes.
package content

import (
	"net/http"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/types"
)

type (
	// TemplatePath identifies a template file passed to the renderer.
	TemplatePath = types.TemplatePath

	// ContentManagerService groups templates by response type and supplies response callbacks.
	ContentManagerService struct {
		// Layouts maps response types to outer template files.
		Layouts map[string][]TemplatePath
		// Contents maps response types to page and fragment template files.
		Contents map[string][]TemplatePath
		// Components maps response types to reusable helper template files.
		Components map[string][]TemplatePath

		// HomeURL is the default home-page redirect destination.
		HomeURL string
		// AuthURL is the login-page redirect destination.
		AuthURL string
		// MessageURL holds an optional application message-page address.
		MessageURL string
		// ErrorURL holds an optional application error-page address.
		ErrorURL string
		// NotFoundURL holds an optional application not-found-page address.
		NotFoundURL string

		// HandleMessage displays plain text. HTML renderers must escape the message.
		HandleMessage func(w http.ResponseWriter, r *http.Request, message string)
		// HandleError presents a public message with an error status and logs err.
		// Callers must remove secrets from err. Pass nil if already logged.
		// Call before writing a response.
		HandleError func(w http.ResponseWriter, r *http.Request, message string, err error, httpStatus int)
		// HandleNotFound renders a response for an unknown resource.
		HandleNotFound http.HandlerFunc

		// AppName is the name supplied to templates.
		AppName string
		// AppTitle is the default page title.
		AppTitle string

		// RenderNotifications enables retrieval of session-backed notifications during rendering.
		RenderNotifications bool
	}
)

const (
	// PageType should be used when an entire page is rendered
	PageType = "page"
	// HtmxType should be used when an HTMX response is required
	HtmxType = "htmx"

	// DefaultAppName is the application name used before configuration.
	DefaultAppName = "HyperServer"
	// DefaultAppTitle is the default page title.
	DefaultAppTitle = "HyperServer"

	// HomeDefault is the default home-page path.
	HomeDefault = "/"
)

// NewContentManager allocates template maps and sets application and home-page defaults.
// Response callbacks are left nil and must be supplied by the application.
func NewContentManager() *ContentManagerService {
	cm := ContentManagerService{}
	cm.Layouts = make(map[string][]TemplatePath)
	cm.Contents = make(map[string][]TemplatePath)
	cm.Components = make(map[string][]TemplatePath)

	cm.HomeURL = HomeDefault

	cm.AppName = DefaultAppName
	cm.AppTitle = DefaultAppTitle

	// default value
	cm.RenderNotifications = false

	return &cm
}

// Configure copies the application name and notification-rendering setting from cfg.
func (c *ContentManagerService) Configure(cfg *config.Config) {
	if cfg.App.Name != "" {
		c.AppName = cfg.App.Name
	}

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

// AddHtmxLayouts appends layout templates for HtmxType responses.
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
