package blog

import (
	"errors"
	"html/template"
	"net/http"
	"slices"
	"strconv"
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
	HTML             template.HTML
	Error            string
	Parent           *Page
	Children         []Page
	Parents          []Page
	DisplayOrderText string
}

// pageLink is one navigation branch. Hidden ancestors hide their entire branch.
type pageLink struct {
	Page
	Children []pageLink
}

// pageTree groups already ordered metadata without loading Markdown bodies.
func pageTree(pages []Page) []pageLink {
	byParent := make(map[string][]Page)
	for _, p := range pages {
		byParent[p.ParentID] = append(byParent[p.ParentID], p)
	}
	var branch func(string) []pageLink
	branch = func(parent string) []pageLink {
		var links []pageLink
		for _, p := range byParent[parent] {
			links = append(links, pageLink{Page: p, Children: branch(p.ID)})
		}
		return links
	}
	return branch("")
}

// pages lists published pages and drafts for the administrator.
func (m *module) pages(w http.ResponseWriter, r *http.Request) {
	pages, err := m.pageStore.ListPages(r.Context(), true)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	page := m.views["pages"].Content(r, pageTree(pages))
	page.Title = "Pages"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display pages.", err)
	}
}

// page displays content and its selected child previews. About and Projects have short URLs.
func (m *module) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	slug := r.PathValue("slug")
	if r.URL.Path == "/about" || r.URL.Path == "/projects" {
		slug = strings.TrimPrefix(r.URL.Path, "/")
	}
	admin := m.access.IsAdmin(user.GetUserFromContext(r.Context()))
	p, err := m.pageStore.GetPageBySlug(r.Context(), slug, admin)
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	html, err := markdown(p.Markdown)
	if err != nil {
		m.app.ContentManager.HandleError(w, r, "Unable to display this page.", err, http.StatusInternalServerError)
		return
	}
	data := pageContent{Page: p, HTML: html}
	if p.ParentID != "" {
		parent, err := m.pageStore.GetPageByID(r.Context(), p.ParentID, admin)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		data.Parent = &parent
	}
	if p.ChildrenDisplay != "hidden" {
		pages, err := m.pageStore.ListPages(r.Context(), admin)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		for _, child := range pages {
			if child.ParentID == p.ID {
				data.Children = append(data.Children, child)
			}
		}
	}
	page := m.views["page"].Content(r, data)
	page.Title = p.Title
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// pageEditor opens the shared Markdown editor for a new or existing page.
func (m *module) pageEditor(w http.ResponseWriter, r *http.Request) {
	p := Page{ParentID: r.URL.Query().Get("parent"), ChildrenDisplay: "hidden"}
	if id := r.PathValue("id"); id != "" {
		var err error
		p, err = m.pageStore.GetPageByID(r.Context(), id, true)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
	}
	m.renderPageEditor(w, r, p, "", http.StatusOK)
}

// savePage validates bounded UTF-8 content and saves a published page or draft.
// Empty Markdown is allowed. New pages may omit the slug for an automatic URL.
func (m *module) savePage(w http.ResponseWriter, r *http.Request) {
	// Parse within the route's body limit, then validate field sizes below.
	// This keeps complete drafts available when Markdown exceeds its content limit.
	if err := form.ParseWithOptions(r, form.ParseOptions{MaxFieldBytes: routing.MiB}); err != nil {
		m.renderPageEditor(w, r, Page{ID: r.PathValue("id")}, err.Message, err.StatusCode)
		return
	}
	p := Page{
		ID:              r.PathValue("id"),
		Slug:            strings.TrimSpace(r.PostForm.Get("slug")),
		Title:           strings.TrimSpace(r.PostForm.Get("title")),
		Markdown:        r.PostForm.Get("content"),
		Published:       r.PostForm.Get("published") == "on",
		ParentID:        r.PostForm.Get("parentID"),
		Summary:         strings.TrimSpace(r.PostForm.Get("summary")),
		PreviewImage:    r.PostForm.Get("previewImage"),
		ChildrenDisplay: r.PostForm.Get("childrenDisplay"),
	}
	if p.ChildrenDisplay == "" {
		p.ChildrenDisplay = "hidden"
	}
	message := validatePage(p)
	if value := strings.TrimSpace(r.PostForm.Get("displayOrder")); value != "" {
		order, err := strconv.Atoi(value)
		if err == nil && order >= 0 && order <= 1_000_000 {
			p.DisplayOrder = &order
		} else if message == "" {
			message = "Enter a display order from 0 to 1,000,000, or leave it blank."
		}
	}
	if message == "" && r.PostForm.Get("published") != "" && r.PostForm.Get("published") != "on" {
		message = "Choose whether to publish the page."
	}
	if message != "" {
		m.renderPageEditor(w, r, p, message, http.StatusUnprocessableEntity)
		return
	}
	if p.PreviewImage != "" {
		image, err := m.images.Open(r.Context(), p.PreviewImage)
		if err != nil {
			if errors.Is(err, errNotFound) {
				m.renderPageEditor(w, r, p, "Upload a valid preview image or remove it to use the default.", http.StatusUnprocessableEntity)
			} else {
				logger.LogRequestError(r, err)
				m.renderPageEditor(w, r, p, "Unable to read the preview image. Please try again.", http.StatusInternalServerError)
			}
			return
		}
		image.Content.Close()
	}

	saved, err := m.pageStore.SavePage(r.Context(), p)
	if errors.Is(err, errPageSlugTaken) {
		m.renderPageEditor(w, r, p, err.Error(), http.StatusConflict)
		return
	}
	if errors.Is(err, errPageParent) {
		m.renderPageEditor(w, r, p, err.Error(), http.StatusUnprocessableEntity)
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
	w.Header().Set("HX-Trigger", "blog:content-saved")
	util.RedirectToURL(w, r, saved.URL())
}

// validatePage returns the first invalid-field message, or an empty string.
func validatePage(p Page) string {
	if p.Title == "" || !utf8.ValidString(p.Title) || utf8.RuneCountInString(p.Title) > 240 {
		return "Enter a title of at most 240 characters."
	}
	if (p.ID != "" || p.Slug != "") && !validSlug(p.Slug) {
		return "Use a slug of at most 120 lowercase letters, numbers, and single hyphens."
	}
	if !utf8.ValidString(p.Markdown) || len(p.Markdown) > maxMarkdownBytes {
		return "Enter valid Markdown of at most 256 KiB."
	}
	if !utf8.ValidString(p.Summary) || utf8.RuneCountInString(p.Summary) > 2000 {
		return "Enter a preview description of at most 2,000 characters."
	}
	if p.ChildrenDisplay != "hidden" && p.ChildrenDisplay != "list" && p.ChildrenDisplay != "blocks" {
		return "Choose hidden, preview list, or preview blocks for subpages."
	}
	return ""
}

// renderPageEditor keeps the HTMX editor mounted on errors. Ordinary requests
// redisplay submitted values, including the publication setting.
func (m *module) renderPageEditor(w http.ResponseWriter, r *http.Request, p Page, message string, status int) {
	data := pageContent{Page: p, Error: message}
	if p.DisplayOrder != nil {
		data.DisplayOrderText = strconv.Itoa(*p.DisplayOrder)
	}
	if r.Method == http.MethodPost {
		// Keep invalid input available for correction, not just parsed numbers.
		data.DisplayOrderText = r.PostForm.Get("displayOrder")
	}
	page := m.views["pageEditor"].Content(r, data)
	if page.IsHtmx() && status >= 400 {
		page = m.views["message"].Content(r, message)
	} else {
		pages, err := m.pageStore.ListPages(r.Context(), true)
		if err != nil && status < 400 {
			m.storageError(w, r, err)
			return
		}
		if err != nil {
			logger.LogRequestError(r, err)
			pages = nil
		}
		// Descendants cannot be parents. Exclude the complete branch from choices.
		var choices func([]pageLink)
		choices = func(links []pageLink) {
			for _, link := range links {
				if link.ID != p.ID {
					data.Parents = append(data.Parents, link.Page)
					choices(link.Children)
				}
			}
		}
		choices(pageTree(pages))
		if p.ParentID != "" && !slices.ContainsFunc(data.Parents, func(parent Page) bool { return parent.ID == p.ParentID }) {
			// Preserve the submitted choice if lookup failed or the parent is invalid.
			data.Parents = append(data.Parents, Page{ID: p.ParentID, Title: "Unavailable parent"})
		}
		page.Data = data
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
	m.renderPageDelete(w, r, "", http.StatusOK)
}

// renderPageDelete keeps conflicts in the open dialog or the full confirmation page.
func (m *module) renderPageDelete(w http.ResponseWriter, r *http.Request, message string, status int) {
	page := m.views["pageDelete"].Content(r, pageContent{Error: message})
	if !page.IsHtmx() || message == "" {
		p, err := m.pageStore.GetPageByID(r.Context(), r.PathValue("id"), true)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		page.Data = pageContent{Page: p, Error: message}
	}
	page.Title = "Delete page"
	page.ResponseStatusCode = status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// deletePage removes content, leaving any uploaded images available to other pages.
func (m *module) deletePage(w http.ResponseWriter, r *http.Request) {
	if err := m.pageStore.DeletePage(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, errPageHasChildren) {
			m.renderPageDelete(w, r, "This page has subpages. Move or delete its subpages before deleting this page.", http.StatusConflict)
			return
		}
		m.storageError(w, r, err)
		return
	}
	util.RedirectToURL(w, r, "/pages")
}
