// Package blog provides a personal Markdown blog with administrator-only publishing.
// The module owns its configuration, authorization rules, templates, and content storage.
package blog

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/trentnix/hyperserver/auth"
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

	// UploadsDirectory holds uploaded images, relative to the application configuration directory.
	UploadsDirectory string `yaml:"uploadsDirectory"`

	// AssetsDirectory optionally serves application-owned files at /local-assets/{name}.
	// The directory must exist. Relative paths use the application configuration directory.
	AssetsDirectory string `yaml:"assetsDirectory"`

	Storage struct {
		Provider   string `yaml:"provider"`
		Connection string `yaml:"connection"`
	} `yaml:"storage"`
}

// Access shares the blog's administrator rule with authentication and rendering.
// Module initialization sets the identity once, before requests are accepted.
type Access struct {
	emailKey string
	setup    *setupState
}

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

// IsAdmin requires the configured account to have completed any required verification.
func (a *Access) IsAdmin(account *user.User) bool {
	return a != nil && a.emailKey != "" && account != nil && !account.NeedsVerification() && user.EmailKey(account.Email) == a.emailKey
}

// module owns one application's views, content repositories, and images.
type module struct {
	site            *Site
	settings        *Settings
	access          *Access
	accounts        user.AccountRepository
	app             *server.ApplicationServer
	auth            *auth.AuthManager
	posts           repository
	pageStore       pageRepository
	searchStore     publicSearchRepository
	tags            tagRepository
	profileStore    profileRepository
	profile         *ProfileState
	images          imageStore
	media           mediaStore
	mediaReferences mediaReferenceRepository
	mediaMetadata   mediaMetadataRepository
	imageAccess     imageAccessRepository
	history         revisionRepository
	localAssets     *os.Root
	close           func() error
	views           map[string]*content.View
	feed            feedCache
	recentPosts     cachedValue[int, []ArchiveEntry]
	archivePosts    cachedValue[int, []ArchiveEntry]
	now             func() time.Time

	// mediaMu prevents cleanup from racing with uploads or saves of image references.
	mediaMu sync.RWMutex
	// imageProcessing bounds decode/resize memory to one upload per application.
	imageProcessing chan struct{}
}

// init registers the factory without opening storage or creating runtime state.
func init() {
	handlers.Register(handlers.Descriptor{Name: "blog", Requires: []handlers.Requirement{{Capability: auth.ManagerCapability}}, New: func() handlers.Handler {
		return &module{settings: &Settings{}}
	}})
}

func (m *module) BindDependencies(d handlers.Dependencies) error {
	var err error
	m.auth, err = handlers.GetDependency[*auth.AuthManager](d, auth.ManagerCapability)
	return err
}

// Configuration supplies the destination for the module's YAML settings.
func (m *module) Configuration() any { return m.settings }

// Init validates settings, prepares views, and opens content storage before serving.
// It initializes the shared administrator rule only after setup succeeds.
func (m *module) Init(ctx context.Context, app *server.ApplicationServer) error {
	if m.access == nil {
		return errors.New("configure the blog through Site.Configure")
	}
	if m.settings == nil {
		return errors.New("blog requires settings")
	}
	if !form.ValidEmail(m.settings.AdminEmail) {
		return errors.New("blog.adminEmail must be a valid email address")
	}
	if m.settings.RecentPostCount < 1 {
		return errors.New("blog.recentPostCount must be greater than zero")
	}
	if !app.Config.Auth.Enabled || m.accounts == nil {
		return errors.New("blog requires authentication and account storage")
	}
	m.app = app
	templates, err := prepareTemplates(app.ContentManager)
	if err != nil {
		return err
	}
	m.views = make(map[string]*content.View)
	for _, name := range []string{
		"post",
		"posts",
		"archive",
		"search",
		"recent",
		"page",
		"pages",
		"pageEditor",
		"pageDelete",
		"editor",
		"formError",
		"delete",
		"message",
		"profileEditor",
		"profileHistory",
		"profileRevisionPreview",
		"history",
		"revisionPreview",
		"revisionDelete",
		"media",
		"mediaLibrary",
		"mediaUploadError",
		"mediaDetail",
		"mediaDelete",
		"mediaDeleteAll",
		"mediaDeleteResult",
	} {
		view, err := templates.View(app.ContentManager, "blog."+name)
		if err != nil {
			return err
		}
		m.views[name] = view
	}

	store, closeStore, err := openRepository(ctx, *m.settings, app.Config.App.WorkingDirectory)
	if err != nil {
		return fmt.Errorf("open blog storage: %w", err)
	}
	setup, err := newSetupState(ctx, store, m.accounts, user.EmailKey(m.settings.AdminEmail))
	if err != nil {
		return errors.Join(fmt.Errorf("load blog setup: %w", err), closeStore())
	}
	profile, err := store.GetProfile(ctx)
	if errors.Is(err, errNotFound) {
		profile, err = Profile{Name: "My Blog"}, nil
	}
	if err == nil {
		err = profile.validate()
	}
	if err != nil {
		return errors.Join(fmt.Errorf("load blog profile: %w", err), closeStore())
	}
	images, err := openImageStore(m.settings.UploadsDirectory, app.Config.App.WorkingDirectory)
	if err != nil {
		return errors.Join(fmt.Errorf("open blog uploads: %w", err), closeStore())
	}
	assets, err := openLocalAssets(m.settings.AssetsDirectory, app.Config.App.WorkingDirectory)
	if err != nil {
		return errors.Join(fmt.Errorf("open blog assets: %w", err), images.Close(), closeStore())
	}
	if err := validateAssetSeparation(images.root, assets); err != nil {
		return errors.Join(err, assets.Close(), images.Close(), closeStore())
	}
	m.posts, m.images = store, images
	m.imageProcessing = make(chan struct{}, 1)
	m.media, m.mediaReferences = images, store
	m.mediaMetadata = store
	m.pageStore = store
	m.searchStore = store
	m.tags = store
	m.profileStore = store
	m.imageAccess = store
	m.history = store
	if m.profile == nil {
		m.profile = &ProfileState{}
	}
	m.profile.mu.Lock()
	m.profile.value = profile
	m.profile.mu.Unlock()
	m.localAssets = assets
	m.close = func() error {
		var assetError error
		if assets != nil {
			assetError = assets.Close()
		}
		return errors.Join(assetError, images.Close(), closeStore())
	}
	m.access.emailKey = user.EmailKey(m.settings.AdminEmail)
	m.access.setup = setup
	if m.site != nil {
		m.site.populateNavigation = m.populateNavigation
	}
	return nil
}

// Shutdown closes content and image storage after requests have drained.
// It must only be called after successful initialization.
func (m *module) Shutdown(context.Context) error { return m.close() }

// adminPageKey marks management responses without confusing them with public pages viewed by an administrator.
type adminPageKey struct{}

// requireAdmin rejects accounts that do not satisfy the administrator rule, marks management
// responses for rendering, and prevents caching, including access denials.
func (m *module) requireAdmin(next http.HandlerFunc) http.Handler {
	return m.requireSetup(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), adminPageKey{}, true))
		w.Header().Set("Cache-Control", "no-store")
		if !m.access.IsAdmin(user.GetUserFromContext(r.Context())) {
			m.app.ContentManager.HandleError(w, r, "Sign in as the administrator and complete any required email verification.", nil, http.StatusForbidden)
			return
		}
		next(w, r)
	})
}
