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

// pageContent pairs stored content with safe HTML or an editor error.
type pageContent struct {
	Page
	HTML  template.HTML
	Error string
}

// pages lists published pages and drafts for the administrator.
func (m *module) pages(w http.ResponseWriter, r *http.Request) {
	pages, err := m.pageStore.ListPages(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	page := m.views["pages"].Content(r, pages)
	page.Title = "Pages"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display pages.", err)
	}
}

// page displays standalone content. /about is an alias for the about slug.
func (m *module) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	slug := r.PathValue("slug")
	if r.URL.Path == "/about" {
		slug = "about"
	}
	p, err := m.pageStore.GetPageBySlug(r.Context(), slug, m.access.IsAdmin(user.GetUserFromContext(r.Context())))
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	html, err := markdown(p.Markdown)
	if err != nil {
		m.app.ContentManager.HandleError(w, r, "Unable to display this page.", err, http.StatusInternalServerError)
		return
	}
	page := m.views["page"].Content(r, pageContent{Page: p, HTML: html})
	page.Title = p.Title
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// pageEditor opens the shared Markdown editor for a new or existing page.
func (m *module) pageEditor(w http.ResponseWriter, r *http.Request) {
	var p Page
	if id := r.PathValue("id"); id != "" {
		var err error
		p, err = m.pageStore.GetPageByID(r.Context(), id)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
	}
	m.renderPageEditor(w, r, p, "", http.StatusOK)
}

// savePage validates bounded UTF-8 content and saves a published page or draft.
// Empty Markdown is allowed. A slug is required even when creating a page.
func (m *module) savePage(w http.ResponseWriter, r *http.Request) {
	// Parse within the route's body limit, then validate field sizes below.
	// This keeps complete drafts available when Markdown exceeds its content limit.
	if err := form.ParseWithOptions(r, form.ParseOptions{MaxFieldBytes: routing.MiB}); err != nil {
		m.renderPageEditor(w, r, Page{ID: r.PathValue("id")}, err.Message, err.StatusCode)
		return
	}
	p := Page{
		ID:        r.PathValue("id"),
		Slug:      strings.TrimSpace(r.PostForm.Get("slug")),
		Title:     strings.TrimSpace(r.PostForm.Get("title")),
		Markdown:  r.PostForm.Get("content"),
		Published: r.PostForm.Get("published") == "on",
	}
	message := validatePage(p)
	if message == "" && r.PostForm.Get("published") != "" && r.PostForm.Get("published") != "on" {
		message = "Choose whether to publish the page."
	}
	if message != "" {
		m.renderPageEditor(w, r, p, message, http.StatusUnprocessableEntity)
		return
	}

	saved, err := m.pageStore.SavePage(r.Context(), p)
	if errors.Is(err, errPageSlugTaken) {
		m.renderPageEditor(w, r, p, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		if errors.Is(err, errNotFound) {
			m.storageError(w, r, err)
			return
		}
		logger.LogRequestError(r, err)
		m.renderPageEditor(w, r, p, "Unable to save this page. Please try again.", http.StatusInternalServerError)
		return
	}
	util.RedirectToURL(w, r, saved.URL())
}

// validatePage returns the first invalid-field message, or an empty string.
func validatePage(p Page) string {
	if p.Title == "" || !utf8.ValidString(p.Title) || utf8.RuneCountInString(p.Title) > 240 {
		return "Enter a title of at most 240 characters."
	}
	if !validSlug(p.Slug) {
		return "Use a slug of at most 120 lowercase letters, numbers, and single hyphens."
	}
	if !utf8.ValidString(p.Markdown) || len(p.Markdown) > maxMarkdownBytes {
		return "Enter valid Markdown of at most 256 KiB."
	}
	return ""
}

// renderPageEditor keeps the HTMX editor mounted on errors. Ordinary requests
// redisplay submitted values, including the publication setting.
func (m *module) renderPageEditor(w http.ResponseWriter, r *http.Request, p Page, message string, status int) {
	page := m.views["pageEditor"].Content(r, pageContent{Page: p, Error: message})
	if page.IsHtmx() && status >= 400 {
		page = m.views["message"].Content(r, message)
	}
	page.Title = "Edit page"
	if p.ID == "" {
		page.Title = "New page"
	}
	page.ResponseStatusCode = status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display the page editor.", err)
	}
}

// confirmPageDelete asks for confirmation without changing storage.
func (m *module) confirmPageDelete(w http.ResponseWriter, r *http.Request) {
	p, err := m.pageStore.GetPageByID(r.Context(), r.PathValue("id"))
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	page := m.views["pageDelete"].Content(r, p)
	page.Title = "Delete page"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// deletePage removes content, leaving any uploaded images available to other pages.
func (m *module) deletePage(w http.ResponseWriter, r *http.Request) {
	if err := m.pageStore.DeletePage(r.Context(), r.PathValue("id")); err != nil {
		m.storageError(w, r, err)
		return
	}
	util.RedirectToURL(w, r, "/pages")
}
