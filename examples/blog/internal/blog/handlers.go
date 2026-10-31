package blog

import (
	"errors"
	"html/template"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// postPage pairs stored post data with HTML produced by the Markdown renderer.
type postPage struct {
	Post
	HTML       template.HTML
	HasHistory bool
	Scheduled  bool
}

// editorPage retains a draft and any user-facing error for the editing form.
type editorPage struct {
	Post
	revisionSelection
	Error                      string
	PublishedSlug              string
	ScheduledSlug              string
	ScheduledAt                int64
	Publication                string
	ScheduleDate, ScheduleTime string
	CanSchedule                bool
	TagSuggestions             []string
}

// PreviewField configures the shared image picker for post sharing.
func (p editorPage) PreviewField() previewImageField {
	return previewImageField{
		InputID: "post-image", Value: p.PreviewImage, URL: p.PreviewURL(), Optional: true,
		Description: "For sharing links, not the post body.",
		Help:        "By default, the preview uses the first suitable image in the post, or the site portrait if none is available.",
	}
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
	data.TagSuggestions = m.tagSuggestions(r)
	selection, err := m.selectRevision(r, "post", p.ID, "/post/s/"+p.Slug, Revision{Title: p.Title, Summary: p.Summary, Markdown: p.Markdown, PreviewImage: p.PreviewImage, Tags: p.Tags})
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	data.revisionSelection = selection
	if selected := selection.LoadedRevision; selected != nil {
		data.Title, data.Summary, data.Markdown, data.PreviewImage, data.Tags = selected.Title, selected.Summary, selected.Markdown, selected.PreviewImage, selected.Tags
	}
	page := m.views["editor"].Content(r, data)
	page.Title = "New post"
	if p.ID != "" {
		page.Title = "Edit post"
	}
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// save validates content, saves its selected publication state, and redirects to its slug URL.
// Validation and storage failures keep parsed draft values available for correction.
func (m *module) save(w http.ResponseWriter, r *http.Request) {
	// Parse within the route's body limit. Content validation then retains the
	// complete submitted draft when Markdown exceeds its smaller writing limit.
	if err := form.ParseWithOptions(r, form.ParseOptions{MaxFieldBytes: routing.MiB}); err != nil {
		m.editorError(w, r, Post{ID: r.PathValue("id"), Draft: true}, err.Message, err.StatusCode)
		return
	}
	p := Post{
		ID:           r.PathValue("id"),
		Slug:         strings.TrimSpace(r.PostForm.Get("slug")),
		Title:        strings.TrimSpace(r.PostForm.Get("title")),
		Summary:      strings.TrimSpace(r.PostForm.Get("summary")),
		Tags:         r.PostForm.Get("tags"),
		Markdown:     r.PostForm.Get("content"),
		PreviewImage: r.PostForm.Get("previewImage"),
		Draft:        r.PostForm.Get("published") != "on",
	}
	if version, valid := editorVersion(r); valid {
		p.Version = version
	}
	publicationError := m.readPublication(r, &p)
	if publicationError != "" {
		m.editorError(w, r, p, publicationError, http.StatusUnprocessableEntity)
		return
	}
	saved, err := m.writePost(r.Context(), p)
	var invalid contentError
	if errors.As(err, &invalid) || errors.Is(err, errMissingMedia) {
		m.editorError(w, r, p, err.Error(), http.StatusUnprocessableEntity)
		return
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
	w.Header().Set("HX-Trigger", "blog:content-saved")
	util.RedirectToURL(w, r, "/post/s/"+saved.Slug)
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
	} else {
		data.TagSuggestions = m.tagSuggestions(r)
		if p.ID != "" {
			stored, err := m.posts.GetByID(r.Context(), p.ID, true)
			if err != nil {
				logger.LogRequestError(r, err)
			} else {
				publication := m.postEditorData(stored)
				data.PublishedSlug, data.CanSchedule = publication.PublishedSlug, publication.CanSchedule
				data.ScheduledSlug, data.ScheduledAt = publication.ScheduledSlug, publication.ScheduledAt
			}
		}
		page.Data = data
	}
	page.Title = "New post"
	if p.ID != "" {
		page.Title = "Edit post"
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
	m.invalidatePosts()
	m.finishContentDelete(w, r, "post", "/post/manage")
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
