package blog

import (
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

const maxMarkdownBytes = 256 * 1024

const slugConflictMessage = "This URL slug is already in use. Choose a different one, or leave it blank to generate one from the title."

// Routes binds public reading and explicitly protected publishing endpoints.
func (m *module) Routes(routes *routing.Routes) error {
	assets, err := fs.Sub(blogModuleFS, "static")
	if err != nil {
		return err
	}
	routes.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
	if m.localAssets != nil {
		routes.HandleFunc("GET /local-assets/{name}", m.localAsset)
	}
	routes.HandleFunc("GET /{$}", m.index)
	routes.HandleFunc("GET /posts", m.archive)
	routes.HandleFunc("GET /posts/recent", m.recent)
	routes.HandleFunc("GET /post/s/{slug}", m.post)
	routes.HandleFunc("GET /post/id/{id}", m.post)
	routes.HandleFunc("GET /images/{id}", m.image)
	routes.HandleFunc("GET /rss", m.rss)
	routes.HandleFunc("GET /sitemap.xml", m.sitemap)
	routes.HandleFunc("GET /robots.txt", m.robots)
	routes.Handle("GET /history/{kind}/{id}", m.requireAdmin(m.revisions))
	routes.Handle("GET /history/{kind}/{id}/delete/{revision}", m.requireAdmin(m.confirmRevisionDelete))
	routes.Handle("POST /history/{kind}/{id}/delete/{revision}", m.requireAdmin(m.deleteRevision))
	routes.Handle("GET /profile", m.requireAdmin(m.profileEditor))
	routes.Handle("GET /media", m.requireAdmin(m.mediaLibrary))
	routes.Handle("GET /media/{id}", m.requireAdmin(m.mediaDetail))
	routes.Handle("POST /media/{id}", m.requireAdmin(m.renameMedia))
	routes.Handle("GET /media/unused", m.requireAdmin(m.unusedMedia))
	routes.Handle("GET /media/delete-unused", m.requireAdmin(m.confirmUnusedMediaDelete))
	routes.HandleWithBodyLimit("POST /media/delete-unused", m.requireAdmin(m.deleteUnusedMedia), routing.MiB)
	routes.Handle("GET /media/delete/{id}", m.requireAdmin(m.confirmMediaDelete))
	routes.Handle("POST /media/delete/{id}", m.requireAdmin(m.deleteMedia))
	routes.HandleWithBodyLimit("POST /profile", m.requireAdmin(m.saveProfile), routing.MiB)
	routes.HandleFunc("GET /about", m.page)
	routes.HandleFunc("GET /page/{slug}", m.page)
	routes.Handle("GET /pages", m.requireAdmin(m.pages))
	routes.Handle("GET /pages/create", m.requireAdmin(m.pageEditor))
	routes.Handle("GET /pages/update/{id}", m.requireAdmin(m.pageEditor))
	routes.Handle("GET /pages/delete/{id}", m.requireAdmin(m.confirmPageDelete))
	routes.HandleWithBodyLimit("POST /pages", m.requireAdmin(m.savePage), routing.MiB)
	routes.HandleWithBodyLimit("POST /pages/{id}", m.requireAdmin(m.savePage), routing.MiB)
	routes.Handle("POST /pages/delete/{id}", m.requireAdmin(m.deletePage))
	routes.HandleFunc("GET /projects", m.page)
	routes.HandleFunc("GET /project/s/{slug}", m.page) // Retain published project links.
	routes.Handle("GET /post/create", m.requireAdmin(m.editor))
	routes.Handle("GET /post/manage", m.requireAdmin(m.managePosts))
	routes.Handle("POST /post/publish/{id}", m.requireAdmin(m.publish))
	routes.Handle("POST /post/unpublish/{id}", m.requireAdmin(m.unpublish))
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

// login redirects the short login URL to the email authentication page.
func (m *module) login(w http.ResponseWriter, r *http.Request) {
	util.RedirectToURL(w, r, loginURL)
}

// index displays the newest post, or an empty-state message when none exist.
func (m *module) index(w http.ResponseWriter, r *http.Request) {
	post, err := m.posts.Latest(r.Context())
	if errors.Is(err, errNotFound) {
		page := m.views["message"].Content(r, "No posts yet.")
		m.setSharing(page, r, sharingPreview{URL: "/", Type: "website"}, "")
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
	// A saved draft uses the normal post view, but only its administrator can read it.
	w.Header().Set("Cache-Control", "no-store")
	includeDrafts := m.access.IsAdmin(user.GetUserFromContext(r.Context()))
	var p Post
	var err error
	if slug := r.PathValue("slug"); slug != "" {
		p, err = m.posts.GetBySlug(r.Context(), slug, includeDrafts)
	} else {
		p, err = m.posts.GetByID(r.Context(), r.PathValue("id"), includeDrafts)
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
	data := postPage{Post: p, HTML: html, Scheduled: p.Scheduled(m.currentTime())}
	if m.access.IsAdmin(user.GetUserFromContext(r.Context())) {
		data.HasHistory, err = m.history.HasHistory(r.Context(), "post", p.ID)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
	}
	page := m.views["post"].Content(r, data)
	page.Title = p.Title
	if r.URL.Path == "/" {
		m.setSharing(page, r, sharingPreview{URL: "/", Type: "website"}, "")
	} else if !p.Draft && !data.Scheduled {
		m.setSharing(page, r, sharingPreview{Title: p.Title, Description: p.Summary, URL: "/post/s/" + p.Slug, Image: p.PreviewURL(), Type: "article"}, html)
	}
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
	m.setSharing(page, r, sharingPreview{Title: page.Title, URL: "/posts", Type: "website"}, "")
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// recent displays the configured number of newest links for the sidebar or footer.
func (m *module) recent(w http.ResponseWriter, r *http.Request) {
	posts, err := m.recentPosts.loadUntil(r.Context(), m.settings.RecentPostCount, func() ([]ArchiveEntry, time.Time, error) {
		next, err := m.posts.NextPublication(r.Context())
		if err != nil {
			return nil, time.Time{}, err
		}
		posts, err := m.posts.ListRecent(r.Context(), m.settings.RecentPostCount)
		return posts, next, err
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
	p := Post{Draft: true}
	if id := r.PathValue("id"); id != "" {
		var err error
		p, err = m.posts.GetByID(r.Context(), id, true)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
	}

	data := m.postEditorData(p)
	selection, err := m.selectRevision(r, "post", p.ID, "/post/s/"+p.Slug, Revision{Title: p.Title, Summary: p.Summary, Markdown: p.Markdown, PreviewImage: p.PreviewImage})
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	data.revisionSelection = selection
	if selected := selection.LoadedRevision; selected != nil {
		data.Title, data.Summary, data.Markdown, data.PreviewImage = selected.Title, selected.Summary, selected.Markdown, selected.PreviewImage
	}
	page := m.views["editor"].Content(r, data)
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// save validates content, saves its selected publication state, and redirects to its slug URL.
// Validation and storage failures keep parsed draft values available for correction.
func (m *module) save(w http.ResponseWriter, r *http.Request) {
	m.mediaMu.RLock()
	defer m.mediaMu.RUnlock()

	if err := form.ParseWithOptions(r, form.ParseOptions{MaxFieldBytes: maxMarkdownBytes}); err != nil {
		m.editorError(w, r, Post{ID: r.PathValue("id"), Draft: true}, err.Message, err.StatusCode)
		return
	}
	p := Post{
		ID:           r.PathValue("id"),
		Slug:         strings.TrimSpace(r.PostForm.Get("slug")),
		Title:        strings.TrimSpace(r.PostForm.Get("title")),
		Summary:      strings.TrimSpace(r.PostForm.Get("summary")),
		Markdown:     r.PostForm.Get("content"),
		PreviewImage: r.PostForm.Get("previewImage"),
		Draft:        r.PostForm.Get("published") != "on",
	}
	version, validVersion := editorVersion(r)
	p.Version = version
	publicationError := m.readPublication(r, &p)
	generatedSlug := p.Slug == ""
	if generatedSlug {
		p.Slug = slugFromTitle(p.Title)
	}
	message := validatePost(p)
	if message == "" {
		message = publicationError
	}
	if message != "" {
		m.editorError(w, r, p, message, http.StatusUnprocessableEntity)
		return
	}
	if !validVersion {
		m.editorError(w, r, p, editConflictMessage, http.StatusConflict)
		return
	}
	if p.PreviewImage != "" {
		image, err := m.images.Open(r.Context(), p.PreviewImage)
		if err != nil {
			if errors.Is(err, errNotFound) {
				m.editorError(w, r, p, "Upload a valid preview image or remove it to use automatic selection.", http.StatusUnprocessableEntity)
			} else {
				logger.LogRequestError(r, err)
				m.editorError(w, r, p, "Unable to read the preview image. Please try again.", http.StatusInternalServerError)
			}
			return
		}
		image.Content.Close()
	}

	if err := m.checkContentImages(r, p.Markdown, "/post/s/"+p.Slug); err != nil {
		message, status := "Unable to check uploaded images. Please try again.", http.StatusInternalServerError
		if errors.Is(err, errMissingMedia) {
			message, status = err.Error(), http.StatusUnprocessableEntity
		} else {
			logger.LogRequestError(r, err)
		}
		m.editorError(w, r, p, message, status)
		return
	}
	saved, err := m.posts.Save(r.Context(), p)
	// Let the database settle competing writes. Only generated slugs may change.
	for suffix := 2; generatedSlug && errors.Is(err, errSlugTaken); suffix++ {
		if err = r.Context().Err(); err != nil {
			break
		}
		candidate := p
		candidate.Slug = numberedSlug(p.Slug, suffix)
		saved, err = m.posts.Save(r.Context(), candidate)
	}
	if errors.Is(err, errSlugTaken) {
		m.editorError(w, r, p, slugConflictMessage, http.StatusConflict)
		return
	}
	if errors.Is(err, errEditConflict) {
		m.editorError(w, r, p, editConflictMessage, http.StatusConflict)
		return
	}
	if errors.Is(err, errAlreadyPublished) {
		m.editorError(w, r, p, "This post has already been published. Choose Draft or Now.", http.StatusUnprocessableEntity)
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
	w.Header().Set("HX-Trigger", "blog:content-saved")
	util.RedirectToURL(w, r, "/post/s/"+saved.Slug)
}

// validatePost returns the first user-facing validation error, or an empty string.
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
	if !validSlug(p.Slug) {
		return "Use a slug of at most 120 lowercase letters, numbers, and single hyphens."
	}
	return ""
}

// slugFromTitle uses lowercase ASCII letters and digits, separates other runs
// with hyphens, and caps the result at 120 bytes. An empty result needs a custom slug.
func slugFromTitle(title string) string {
	slug := make([]byte, 0, 120)
	for _, ch := range title {
		if len(slug) == 120 {
			break
		}
		if ch >= 'A' && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		if ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' {
			slug = append(slug, byte(ch))
		} else if len(slug) > 0 && slug[len(slug)-1] != '-' {
			slug = append(slug, '-')
		}
	}
	return strings.TrimRight(string(slug), "-")
}

// numberedSlug appends a collision suffix to a valid base without exceeding 120 bytes.
func numberedSlug(base string, number int) string {
	suffix := "-" + strconv.Itoa(number)
	if len(base)+len(suffix) > 120 {
		base = base[:120-len(suffix)]
	}
	return strings.TrimRight(base, "-") + suffix
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
	// Keep an omitted slug automatic when the user corrects another field.
	p.Slug = strings.TrimSpace(r.PostForm.Get("slug"))
	data := editorPage{Post: p, CanSchedule: true, Error: message, revisionSelection: editorRevision(r, "post", p.ID)}
	data.Publication = r.PostForm.Get("published")
	data.ScheduleDate, data.ScheduleTime = r.PostForm.Get("publishDate"), r.PostForm.Get("publishTime")
	page := m.views["editor"].Content(r, data)
	if page.IsHtmx() {
		page = m.views["message"].Content(r, message)
	} else if p.ID != "" {
		stored, err := m.posts.GetByID(r.Context(), p.ID, true)
		if err != nil {
			logger.LogRequestError(r, err)
		} else {
			publication := m.postEditorData(stored)
			data.PublishedSlug, data.CanSchedule = publication.PublishedSlug, publication.CanSchedule
			data.ScheduledSlug, data.ScheduledAt = publication.ScheduledSlug, publication.ScheduledAt
		}
		page.Data = data
	}
	page.ResponseStatusCode = status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display the form error.", err)
	}
}

// confirmDelete displays the selected post without changing storage.
func (m *module) confirmDelete(w http.ResponseWriter, r *http.Request) {
	p, err := m.posts.GetByID(r.Context(), r.PathValue("id"), true)
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	page := m.views["delete"].Content(r, p)
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// delete removes an existing post and returns to post management only on success.
func (m *module) delete(w http.ResponseWriter, r *http.Request) {
	if err := m.posts.Delete(r.Context(), r.PathValue("id")); err != nil {
		m.storageError(w, r, err)
		return
	}
	m.feed.invalidate()
	m.recentPosts.invalidate()
	util.RedirectToURL(w, r, "/post/manage")
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
