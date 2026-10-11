// Package content configures shared templates and response handlers for rendering.
// Configure a ContentManagerService before serving requests. Its maps and callbacks
// are mutable and are not protected against concurrent configuration changes.
package content

import (
	"io/fs"
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

		// BuildLayoutData optionally supplies request-specific data for shared layouts.
		// Configure it before serving requests. It must be concurrency-safe and
		// return request-owned or immutable values. A nil request skips the callback.
		BuildLayoutData func(*http.Request) any

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

// RegisterLayouts replaces layout paths for contentType with paths relative to files.
func (c *ContentManagerService) RegisterLayouts(contentType string, files fs.FS, names ...string) {
	c.Layouts[contentType] = nil
	c.AddLayouts(contentType, files, names...)
}

// RegisterContents replaces content paths for contentType with paths relative to files.
func (c *ContentManagerService) RegisterContents(contentType string, files fs.FS, names ...string) {
	c.Contents[contentType] = nil
	c.AddContents(contentType, files, names...)
}

// RegisterComponents replaces component paths for contentType with paths relative to files.
func (c *ContentManagerService) RegisterComponents(contentType string, files fs.FS, names ...string) {
	c.Components[contentType] = nil
	c.AddComponents(contentType, files, names...)
}

// AddLayout appends a layout path relative to files for contentType.
func (c *ContentManagerService) AddLayout(contentType string, files fs.FS, name string) {
	c.Layouts[contentType] = append(c.Layouts[contentType], TemplatePath{FS: files, Path: name})
}

// AddLayouts appends layout paths relative to files for contentType, in order.
func (c *ContentManagerService) AddLayouts(contentType string, files fs.FS, names ...string) {
	for _, name := range names {
		c.AddLayout(contentType, files, name)
	}
}

// AddContent appends a content path relative to files for contentType.
func (c *ContentManagerService) AddContent(contentType string, files fs.FS, name string) {
	c.Contents[contentType] = append(c.Contents[contentType], TemplatePath{FS: files, Path: name})
}

// AddContents appends content paths relative to files for contentType, in order.
func (c *ContentManagerService) AddContents(contentType string, files fs.FS, names ...string) {
	for _, name := range names {
		c.AddContent(contentType, files, name)
	}
}

// AddComponent appends a component path relative to files for contentType.
func (c *ContentManagerService) AddComponent(contentType string, files fs.FS, name string) {
	c.Components[contentType] = append(c.Components[contentType], TemplatePath{FS: files, Path: name})
}

// AddComponents appends component paths relative to files for contentType, in order.
func (c *ContentManagerService) AddComponents(contentType string, files fs.FS, names ...string) {
	for _, name := range names {
		c.AddComponent(contentType, files, name)
	}
}

// AddPageLayout appends a layout path relative to files for PageType responses.
func (c *ContentManagerService) AddPageLayout(files fs.FS, name string) {
	c.AddLayout(PageType, files, name)
}

// AddPageLayouts appends layout paths relative to files for PageType responses, in order.
func (c *ContentManagerService) AddPageLayouts(files fs.FS, names ...string) {
	c.AddLayouts(PageType, files, names...)
}

// AddHtmxLayout appends a layout path relative to files for HtmxType responses.
func (c *ContentManagerService) AddHtmxLayout(files fs.FS, name string) {
	c.AddLayout(HtmxType, files, name)
}

// AddHtmxLayouts appends layout paths relative to files for HtmxType responses, in order.
func (c *ContentManagerService) AddHtmxLayouts(files fs.FS, names ...string) {
	c.AddLayouts(HtmxType, files, names...)
}

// AddPageContent appends a content path relative to files for PageType responses.
func (c *ContentManagerService) AddPageContent(files fs.FS, name string) {
	c.AddContent(PageType, files, name)
}

// AddPageContents appends content paths relative to files for PageType responses, in order.
func (c *ContentManagerService) AddPageContents(files fs.FS, names ...string) {
	c.AddContents(PageType, files, names...)
}

// AddHtmxContent appends a content path relative to files for HtmxType responses.
func (c *ContentManagerService) AddHtmxContent(files fs.FS, name string) {
	c.AddContent(HtmxType, files, name)
}

// AddHtmxContents appends content paths relative to files for HtmxType responses, in order.
func (c *ContentManagerService) AddHtmxContents(files fs.FS, names ...string) {
	c.AddContents(HtmxType, files, names...)
}

// AddPageComponent appends a component path relative to files for PageType responses.
func (c *ContentManagerService) AddPageComponent(files fs.FS, name string) {
	c.AddComponent(PageType, files, name)
}

// AddPageComponents appends component paths relative to files for PageType responses, in order.
func (c *ContentManagerService) AddPageComponents(files fs.FS, names ...string) {
	c.AddComponents(PageType, files, names...)
}

// AddHtmxComponent appends a component path relative to files for HtmxType responses.
func (c *ContentManagerService) AddHtmxComponent(files fs.FS, name string) {
	c.AddComponent(HtmxType, files, name)
}

// AddHtmxComponents appends component paths relative to files for HtmxType responses, in order.
func (c *ContentManagerService) AddHtmxComponents(files fs.FS, names ...string) {
	c.AddComponents(HtmxType, files, names...)
}
