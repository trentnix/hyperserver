package blog

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

//go:embed templates/*.html static/*
var blogModuleTemplatesFS embed.FS

// Options supplies the blog's services and site settings. Module initialization
// validates these settings but does not initialize or close storage.
type Options struct {
	// Posts is required and must be ready before module initialization.
	Posts Repository
	// Images is required and must be ready before module initialization.
	Images ImageStore
	// Protect is required middleware applied before every editor and upload handler.
	// The request must carry an authenticated user in its context before the blog's
	// allowlist check runs. Protect does not replace the allowlist check.
	Protect func(http.Handler) http.Handler
	// AdminEmails is a required, nonempty allowlist of valid email addresses.
	// Matching uses user.EmailKey. Accounts awaiting required verification cannot edit.
	AdminEmails []string
	// Title is the required site title, also used in the RSS feed.
	Title string
	// Description is the optional RSS channel description.
	Description string
	// PublicOrigin is the required HTTP(S) origin for RSS links, such as
	// https://example.com. It must have no path, user info, query, or fragment.
	// An optional trailing slash is removed during initialization.
	// The module never derives this value from request headers.
	PublicOrigin string
}

// LayoutData supplies the blog shell, including authentication pages.
// Return a request-owned value from ContentManagerService.BuildLayoutData.
type LayoutData struct {
	// Title and Description supply the site's name and sidebar biography.
	Title, Description string
	// Email identifies the signed-in account in the navigation.
	Email string
	// SignedIn selects authenticated navigation instead of the login link.
	SignedIn bool
	// Admin shows editing links. It does not authorize requests.
	Admin bool
}

// Module returns a configured descriptor named "blog". Use handlers.Replace to
// replace the default descriptor registered when this package is imported.
// Each factory creates fresh module state while sharing the supplied services.
// Initialize services before module setup and close them after requests drain.
// Module copies AdminEmails and defers option validation to module initialization.
func Module(options Options) handlers.Descriptor {
	options.AdminEmails = slices.Clone(options.AdminEmails)
	return handlers.Descriptor{Name: "blog", New: func() handlers.Handler { return &module{options: options} }}
}

func init() { handlers.Register(Module(Options{})) }

type module struct {
	options Options
	manager *contentservice.ContentManagerService
	views   map[string]*content.View
	admins  map[string]bool
}

// Init validates dependencies and prepares templates before routes accept traffic.
func (m *module) Init(ctx context.Context, app *server.ApplicationServer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if app == nil || app.ContentManager == nil || nilService(m.options.Posts) || nilService(m.options.Images) || m.options.Protect == nil {
		return errors.New("blog requires rendering, posts, images, and authentication middleware")
	}
	origin, err := config.ParsePublicOrigin(m.options.PublicOrigin)
	if err != nil || origin == nil {
		return errors.New("blog requires an absolute HTTP(S) public origin without a path")
	}
	m.options.PublicOrigin = origin.String()
	if strings.TrimSpace(m.options.Title) == "" {
		return errors.New("blog title is required")
	}
	m.admins = make(map[string]bool)
	for _, email := range m.options.AdminEmails {
		if !form.ValidEmail(email) {
			return errors.New("blog admin allowlist contains an invalid email address")
		}
		m.admins[user.EmailKey(email)] = true
	}
	if len(m.admins) == 0 {
		return errors.New("blog requires an explicit admin email allowlist")
	}
	m.manager = app.ContentManager
	templates, err := content.PrepareTemplates(m.manager, content.TemplatePath{FS: blogModuleTemplatesFS, Path: "templates/blog.html"})
	if err != nil {
		return err
	}
	m.views = make(map[string]*content.View)
	for _, name := range []string{"post", "archive", "recent", "about", "projects", "edit", "delete", "upload", "empty"} {
		view, err := templates.View(m.manager, "blog."+name)
		if err != nil {
			return err
		}
		m.views[name] = view
	}
	return nil
}

// nilService also rejects typed nil values held in a service interface.
func nilService(service any) bool {
	v := reflect.ValueOf(service)
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// ConfigureRendering selects the blog's layouts without requiring storage or a live module.
// Call it before preparing views. The caller supplies LayoutData through
// cm.BuildLayoutData so authentication pages can use the same shell.
// It returns an error if rendering defaults have already been selected, even by this module.
func ConfigureRendering(cm *contentservice.ContentManagerService) error {
	return cm.ApplyDefaults("blog", func(cm *contentservice.ContentManagerService) {
		cm.RegisterLayouts(contentservice.PageType, blogModuleTemplatesFS, "templates/page.html")
		cm.RegisterLayouts(contentservice.HtmxType, blogModuleTemplatesFS, "templates/fragment.html")
	})
}

// isAdmin checks the trusted request user, not the presentation-only LayoutData.
func (m *module) isAdmin(r *http.Request) bool {
	u := user.GetUserFromContext(r.Context())
	return u != nil && !u.NeedsVerification() && m.admins[user.EmailKey(u.Email)]
}

// admin applies application authentication before checking the blog allowlist.
func (m *module) admin(next http.HandlerFunc) http.Handler {
	return m.options.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.isAdmin(r) {
			http.Error(w, "Blog editing is restricted to administrators", http.StatusForbidden)
			return
		}
		next(w, r)
	}))
}

// Routes binds public reading routes and protected editing routes. Only post
// submissions and image uploads receive larger request-body limits.
func (m *module) Routes(routes *routing.Routes) error {
	routes.HandleFunc("GET /{$}", m.home)
	routes.HandleFunc("GET /post/s/{slug}", m.postBySlug)
	routes.HandleFunc("GET /post/id/{id}", m.postByID)
	routes.HandleFunc("GET /posts", m.archive)
	routes.HandleFunc("GET /rss", m.rss)
	routes.HandleFunc("GET /about", func(w http.ResponseWriter, r *http.Request) { m.render(w, r, "about", "About", nil) })
	routes.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) { m.render(w, r, "projects", "Projects", nil) })
	routes.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/auth/login/email", http.StatusSeeOther)
	})
	routes.HandleFunc("GET /images/{id}", m.image)
	routes.HandleFunc("GET /blog-assets/{file}", func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")
		switch file {
		case "blog.css", "forms.css", "blog.js", "htmx.min.js",
			"source-sans-3.ttf", "source-sans-3-italic.ttf",
			"trent-nix-small.jpg", "site-bg.jpg", "site-bg-gray-faded.jpg",
			"about.png", "projects.png", "linkedin.png", "rss.png", "favicon-32x32.png":
		default:
			http.NotFound(w, r)
			return
		}
		http.ServeFileFS(w, r, blogModuleTemplatesFS, "static/"+file)
	})
	routes.Handle("GET /post/create", m.admin(m.createForm))
	// URL encoding can expand each source byte to three bytes. Leave room for
	// the bounded title, summary, slug, and version as well.
	const editBodyLimit = 3*MaxContentBytes + 64*1024
	routes.HandleWithBodyLimit("POST /post", m.admin(m.create), editBodyLimit)
	routes.Handle("GET /post/update/{id}", m.admin(m.editForm))
	routes.HandleWithBodyLimit("POST /post/update/{id}", m.admin(m.update), editBodyLimit)
	routes.Handle("GET /post/delete/{id}", m.admin(m.deleteForm))
	routes.Handle("POST /post/delete/{id}", m.admin(m.delete))
	routes.Handle("GET /post/images/upload", m.admin(m.uploadForm))
	routes.HandleWithBodyLimit("POST /post/images/upload", m.admin(m.upload), MaxImageBytes+64*1024)
	return routes.Err()
}

func (m *module) render(w http.ResponseWriter, r *http.Request, view, title string, data any) {
	page := m.views[view].Content(r, data)
	page.Title = title
	if err := page.Render(w, r); err != nil {
		m.manager.HandleError(w, r, "Unable to display this page", err, http.StatusInternalServerError)
	}
}

// failure maps expected storage errors to HTTP responses and reports unexpected
// failures without exposing provider error details to the visitor.
func (m *module) failure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, ErrConflict) {
		http.Error(w, ErrConflict.Error(), http.StatusConflict)
		return
	}
	m.manager.HandleError(w, r, "Blog storage is unavailable", err, http.StatusServiceUnavailable)
}

func postURL(p Post) string { return "/post/s/" + p.Slug }

type postView struct {
	Post
	HTML  template.HTML
	Admin bool
}

func (m *module) showPost(w http.ResponseWriter, r *http.Request, p Post) {
	html, err := RenderMarkdown(p.Markdown)
	if err != nil {
		m.failure(w, r, fmt.Errorf("render post: %w", err))
		return
	}
	m.render(w, r, "post", p.Title, postView{Post: p, HTML: html, Admin: m.isAdmin(r)})
}

func (m *module) home(w http.ResponseWriter, r *http.Request) {
	posts, err := m.options.Posts.List(r.Context(), 0, 1)
	if err != nil {
		m.failure(w, r, err)
		return
	}
	if len(posts) == 0 {
		m.render(w, r, "empty", m.options.Title, nil)
		return
	}
	m.showPost(w, r, posts[0])
}

func (m *module) postBySlug(w http.ResponseWriter, r *http.Request) {
	p, err := m.options.Posts.GetBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		m.failure(w, r, err)
		return
	}
	m.showPost(w, r, p)
}

func (m *module) postByID(w http.ResponseWriter, r *http.Request) {
	p, err := m.options.Posts.GetByID(r.Context(), r.PathValue("id"))
	if err != nil {
		m.failure(w, r, err)
		return
	}
	m.showPost(w, r, p)
}
