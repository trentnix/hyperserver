package blog

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
)

const maxMarkdownBytes = 256 * 1024

// Routes binds public reading and explicitly protected publishing endpoints.
func (m *module) Routes(routes *routing.Routes) error {
	assets, err := fs.Sub(blogModuleFS, "static")
	if err != nil {
		return err
	}
	routes.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
	routes.HandleFunc("GET /{$}", m.index)
	routes.HandleFunc("GET /posts", m.archive)
	routes.HandleFunc("GET /posts/recent", m.recent)
	routes.HandleFunc("GET /post/s/{slug}", m.post)
	routes.HandleFunc("GET /post/id/{id}", m.post)
	routes.HandleFunc("GET /images/{id}", m.image)
	routes.HandleFunc("GET /rss", m.rss)
	routes.HandleFunc("GET /about", m.about)
	routes.HandleFunc("GET /projects", m.projects)
	routes.HandleFunc("GET /project/s/{slug}", m.project)
	routes.Handle("GET /project/create", m.requireAdmin(m.projectEditor))
	routes.Handle("GET /project/update/{id}", m.requireAdmin(m.projectEditor))
	routes.Handle("GET /project/delete/{id}", m.requireAdmin(m.confirmProjectDelete))
	routes.HandleWithBodyLimit("POST /project", m.requireAdmin(m.saveProject), routing.MiB)
	routes.HandleWithBodyLimit("POST /project/{id}", m.requireAdmin(m.saveProject), routing.MiB)
	routes.Handle("POST /project/delete/{id}", m.requireAdmin(m.deleteProject))
	routes.Handle("GET /post/create", m.requireAdmin(m.editor))
	routes.Handle("GET /post/update/{id}", m.requireAdmin(m.editor))
	routes.Handle("GET /post/delete/{id}", m.requireAdmin(m.confirmDelete))
	routes.HandleWithBodyLimit("POST /post", m.requireAdmin(m.save), routing.MiB)
	routes.HandleWithBodyLimit("POST /post/{id}", m.requireAdmin(m.save), routing.MiB)
	routes.Handle("POST /post/delete/{id}", m.requireAdmin(m.delete))
	routes.HandleWithBodyLimit("POST /post/preview", m.requireAdmin(m.preview), routing.MiB)
	routes.HandleWithBodyLimit("POST /post/images/upload", m.requireAdmin(m.upload), 10*routing.MiB)
	routes.HandleFunc("GET /login", m.login)
	routes.HandleFunc("/", m.app.ContentManager.HandleNotFound)
	return routes.Err()
}

// about displays the author's biography.
func (m *module) about(w http.ResponseWriter, r *http.Request) {
	page := m.views["about"].Content(r, nil)
	page.Title = "About Me"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// login redirects the short login URL to the email authentication page.
func (m *module) login(w http.ResponseWriter, r *http.Request) {
	util.RedirectToURL(w, r, loginURL)
}

// index displays the newest post, or an empty-state message when none exist.
func (m *module) index(w http.ResponseWriter, r *http.Request) {
	post, err := m.posts.Latest(r.Context())
	if errors.Is(err, errNotFound) {
		page := m.views["message"].Content(r, "No posts yet.")
		if err := page.Render(w, r); err != nil {
			page.HandleRenderError(w, r, "Unable to display this page.", err)
		}
		return
	}
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	m.renderPost(w, r, post)
}

// post resolves either a readable slug or the stable post ID used in feed links.
func (m *module) post(w http.ResponseWriter, r *http.Request) {
	var p Post
	var err error
	if slug := r.PathValue("slug"); slug != "" {
		p, err = m.posts.GetBySlug(r.Context(), slug)
	} else {
		p, err = m.posts.GetByID(r.Context(), r.PathValue("id"))
	}
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	m.renderPost(w, r, p)
}

// renderPost converts stored Markdown through the shared renderer before display.
func (m *module) renderPost(w http.ResponseWriter, r *http.Request, p Post) {
	html, err := markdown(p.Markdown)
	if err != nil {
		m.app.ContentManager.HandleError(w, r, "Unable to display this post.", err, http.StatusInternalServerError)
		return
	}
	page := m.views["post"].Content(r, postPage{p, html})
	page.Title = p.Title
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// archive groups post links by month without loading their Markdown bodies.
func (m *module) archive(w http.ResponseWriter, r *http.Request) {
	posts, err := m.posts.ListArchive(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	page := m.views["archive"].Content(r, groupArchive(posts))
	page.Title = "All Posts"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// recent displays the configured number of newest links for the sidebar or footer.
func (m *module) recent(w http.ResponseWriter, r *http.Request) {
	posts, err := m.recentPosts.load(r.Context(), m.settings.RecentPostCount, func() ([]ArchiveEntry, error) {
		return m.posts.ListRecent(r.Context(), m.settings.RecentPostCount)
	})
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	page := m.views["recent"].Content(r, posts)
	page.Title = "Recent Posts"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// editor opens a blank form for creation or loads an existing post for editing.
func (m *module) editor(w http.ResponseWriter, r *http.Request) {
	var p Post
	if id := r.PathValue("id"); id != "" {
		var err error
		p, err = m.posts.GetByID(r.Context(), id)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
	}

	page := m.views["editor"].Content(r, editorPage{Post: p})
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// save validates a submitted draft, persists it, and redirects to its slug URL.
// Validation and storage failures keep parsed draft values available for correction.
func (m *module) save(w http.ResponseWriter, r *http.Request) {
	if err := form.ParseWithOptions(r, form.ParseOptions{MaxFieldBytes: maxMarkdownBytes}); err != nil {
		m.editorError(w, r, Post{ID: r.PathValue("id")}, err.Message, err.StatusCode)
		return
	}
	p := Post{
		ID:       r.PathValue("id"),
		Slug:     strings.TrimSpace(r.PostForm.Get("slug")),
		Title:    strings.TrimSpace(r.PostForm.Get("title")),
		Summary:  strings.TrimSpace(r.PostForm.Get("summary")),
		Markdown: r.PostForm.Get("content"),
	}
	if message := validatePost(p); message != "" {
		m.editorError(w, r, p, message, http.StatusUnprocessableEntity)
		return
	}
	saved, err := m.posts.Save(r.Context(), p)
	if errors.Is(err, errSlugTaken) {
		m.editorError(w, r, p, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		if errors.Is(err, errNotFound) {
			m.storageError(w, r, err)
			return
		}
		logger.LogRequestError(r, err)
		m.editorError(w, r, p, "Unable to save this post. Please try again.", http.StatusInternalServerError)
		return
	}
	m.feed.invalidate()
	m.recentPosts.invalidate()
	util.RedirectToURL(w, r, "/post/s/"+saved.Slug)
}

// validatePost returns the first user-facing validation error, or an empty string.
// Only new posts can omit a slug, which storage then generates.
func validatePost(p Post) string {
	if p.Title == "" || !utf8.ValidString(p.Title) || utf8.RuneCountInString(p.Title) > 240 {
		return "Enter a title of at most 240 characters."
	}
	if !utf8.ValidString(p.Summary) || utf8.RuneCountInString(p.Summary) > 2000 {
		return "Keep the summary under 2,000 characters."
	}
	if strings.TrimSpace(p.Markdown) == "" || len(p.Markdown) > maxMarkdownBytes || !utf8.ValidString(p.Markdown) {
		return "Enter valid Markdown of at most 256 KiB."
	}
	if p.Slug == "" && p.ID == "" {
		return ""
	}
	if !validSlug(p.Slug) {
		return "Use a slug of at most 120 lowercase letters, numbers, and single hyphens."
	}
	return ""
}

// validSlug accepts lowercase ASCII letters and digits separated by single hyphens,
// with no leading or trailing hyphen and a maximum length of 120 bytes.
func validSlug(slug string) bool {
	if slug == "" || len(slug) > 120 || strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") || strings.Contains(slug, "--") {
		return false
	}
	for _, ch := range slug {
		if !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') && ch != '-' {
			return false
		}
	}
	return true
}

// editorError keeps an HTMX editor mounted so its unsaved draft survives errors.
// Ordinary submissions render the form again with the submitted values.
func (m *module) editorError(w http.ResponseWriter, r *http.Request, p Post, message string, status int) {
	page := m.views["editor"].Content(r, editorPage{p, message})
	if page.IsHtmx() {
		page = m.views["message"].Content(r, message)
	}
	page.ResponseStatusCode = status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display the form error.", err)
	}
}

// confirmDelete displays the selected post without changing storage.
func (m *module) confirmDelete(w http.ResponseWriter, r *http.Request) {
	p, err := m.posts.GetByID(r.Context(), r.PathValue("id"))
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	page := m.views["delete"].Content(r, p)
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// delete removes an existing post and redirects to the home page only on success.
func (m *module) delete(w http.ResponseWriter, r *http.Request) {
	if err := m.posts.Delete(r.Context(), r.PathValue("id")); err != nil {
		m.storageError(w, r, err)
		return
	}
	m.feed.invalidate()
	m.recentPosts.invalidate()
	util.RedirectToURL(w, r, "/")
}

// preview renders bounded, UTF-8 Markdown without saving it.
// It uses the same Markdown rules as published posts.
func (m *module) preview(w http.ResponseWriter, r *http.Request) {
	if err := form.ParseWithOptions(r, form.ParseOptions{MaxFieldBytes: maxMarkdownBytes}); err != nil {
		http.Error(w, err.Message, err.StatusCode)
		return
	}
	text := r.PostForm.Get("content")
	if !utf8.ValidString(text) {
		http.Error(w, "Markdown must be valid UTF-8.", http.StatusUnprocessableEntity)
		return
	}
	html, err := markdown(text)
	if err != nil {
		http.Error(w, "Unable to preview the post.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write([]byte(html)); err != nil {
		logger.LogRequestError(r, err)
	}
}

// storageError maps missing records to 404 and keeps other storage details out
// of the public error response.
func (m *module) storageError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errNotFound) {
		m.app.ContentManager.HandleNotFound(w, r)
		return
	}
	m.app.ContentManager.HandleError(w, r, "Unable to access blog storage. Please try again.", err, http.StatusInternalServerError)
}
