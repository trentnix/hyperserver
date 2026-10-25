// Package blog provides a personal Markdown blog with administrator-only publishing.
// The module owns its configuration, authorization rules, templates, and content storage.
package blog

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/server"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

//go:embed templates static
var blogModuleFS embed.FS

// Settings is loaded from modules/blog/config.yaml during module initialization.
type Settings struct {
	AdminEmail string `yaml:"adminEmail"`

	// RecentPostCount limits sidebar and footer links, not the full archive.
	RecentPostCount int `yaml:"recentPostCount"`

	// UploadsDirectory holds public images, relative to the application configuration directory.
	UploadsDirectory string `yaml:"uploadsDirectory"`

	Storage struct {
		Provider   string `yaml:"provider"`
		Connection string `yaml:"connection"`
	} `yaml:"storage"`
}

// Access shares the blog's administrator rule with authentication and rendering.
// Module initialization sets the identity once, before requests are accepted.
type Access struct{ emailKey string }

// AllowAccount permits only the configured administrator to register or log in.
// Password checking and email verification remain the authentication service's job.
func (a *Access) AllowAccount(ctx context.Context, address string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if a == nil || a.emailKey == "" {
		return false, errors.New("blog access is not initialized")
	}
	return user.EmailKey(address) == a.emailKey, nil
}

// IsAdmin requires a verified account matching the configured address.
func (a *Access) IsAdmin(account *user.User) bool {
	return a != nil && a.emailKey != "" && account != nil && account.Verified && user.EmailKey(account.Email) == a.emailKey
}

// module owns one application's views, post and project repositories, and images.
type module struct {
	settings     Settings
	access       *Access
	app          *server.ApplicationServer
	posts        repository
	projectStore projectRepository
	images       imageStore
	close        func() error
	views        map[string]*content.View
	feed         feedCache
	recentPosts  cachedValue[int, []ArchiveEntry]
}

// init registers the factory without opening storage or creating runtime state.
func init() { handlers.Register(Module(nil)) }

// Module creates application-local blog instances using the supplied access rule.
// Each instance opens and closes its content and image storage. Accounts and sessions
// remain framework services and can select storage independently.
func Module(access *Access) handlers.Descriptor {
	return handlers.Descriptor{Name: "blog", New: func() handlers.Handler { return &module{access: access} }}
}

// Configuration supplies the destination for the module's YAML settings.
func (m *module) Configuration() any { return &m.settings }

// Init validates settings, prepares views, and opens content storage before serving.
// It initializes the shared administrator rule only after setup succeeds.
func (m *module) Init(ctx context.Context, app *server.ApplicationServer) error {
	if m.access == nil {
		return errors.New("blog requires an access rule")
	}
	if !form.ValidEmail(m.settings.AdminEmail) {
		return errors.New("blog.adminEmail must be a valid email address")
	}
	if m.settings.RecentPostCount < 1 {
		return errors.New("blog.recentPostCount must be greater than zero")
	}
	if !app.Config.Auth.Enabled || !app.Config.Auth.RegisterRequiresVerification {
		return errors.New("blog requires authentication and email verification")
	}
	m.app = app
	templates, err := content.PrepareTemplates(app.ContentManager, content.TemplatePath{FS: blogModuleFS, Path: pagesFile})
	if err != nil {
		return err
	}
	m.views = make(map[string]*content.View)
	for _, name := range []string{
		"post",
		"archive",
		"recent",
		"about",
		"projects",
		"project",
		"projectEditor",
		"projectDelete",
		"editor",
		"delete",
		"message",
	} {
		view, err := templates.View(app.ContentManager, "blog."+name)
		if err != nil {
			return err
		}
		m.views[name] = view
	}

	store, closeStore, err := openRepository(ctx, m.settings, app.Config.App.WorkingDirectory)
	if err != nil {
		return fmt.Errorf("open blog storage: %w", err)
	}
	images, err := openImageStore(m.settings.UploadsDirectory, app.Config.App.WorkingDirectory)
	if err != nil {
		return errors.Join(fmt.Errorf("open blog uploads: %w", err), closeStore())
	}
	m.posts, m.projectStore, m.images = store, store, images
	m.close = func() error { return errors.Join(images.Close(), closeStore()) }
	m.access.emailKey = user.EmailKey(m.settings.AdminEmail)
	return nil
}

// Shutdown closes content and image storage after requests have drained.
// It must only be called after successful initialization.
func (m *module) Shutdown(context.Context) error { return m.close() }

// requireAdmin rejects unverified or non-administrator accounts and prevents
// caching of publishing responses, including access denials.
func (m *module) requireAdmin(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !m.access.IsAdmin(user.GetUserFromContext(r.Context())) {
			m.app.ContentManager.HandleError(w, r, "A verified administrator account is required.", nil, http.StatusForbidden)
			return
		}
		next(w, r)
	})
}
