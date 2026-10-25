package blog

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

// projectPage pairs a project with safely rendered Markdown or an editor error.
type projectPage struct {
	Project
	HTML  template.HTML
	Error string
}

// projects lists published project cards, including drafts for the administrator.
func (m *module) projects(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	projects, err := m.projectStore.ListProjects(r.Context(), m.access.IsAdmin(user.GetUserFromContext(r.Context())))
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	page := m.views["projects"].Content(r, projects)
	page.Title = "Projects"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this page.", err)
	}
}

// project renders a published project or an administrator's draft without its card image.
func (m *module) project(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, err := m.projectStore.GetProjectBySlug(r.Context(), r.PathValue("slug"), m.access.IsAdmin(user.GetUserFromContext(r.Context())))
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	html, err := markdown(p.Markdown)
	if err != nil {
		m.app.ContentManager.HandleError(w, r, "Unable to display this project.", err, http.StatusInternalServerError)
		return
	}
	page := m.views["project"].Content(r, projectPage{Project: p, HTML: html})
	page.Title = p.Title
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this project.", err)
	}
}

// projectEditor opens the shared Markdown editor with project-specific fields.
func (m *module) projectEditor(w http.ResponseWriter, r *http.Request) {
	var p Project
	if id := r.PathValue("id"); id != "" {
		var err error
		p, err = m.projectStore.GetProjectByID(r.Context(), id)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
	}
	page := m.views["projectEditor"].Content(r, projectPage{Project: p})
	page.Title = "Edit project"
	if p.ID == "" {
		page.Title = "New project"
	}
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display the project editor.", err)
	}
}

// saveProject validates project fields and references only images in managed storage.
// Uploads remain public and independent of the project's publication status.
func (m *module) saveProject(w http.ResponseWriter, r *http.Request) {
	if err := form.ParseWithOptions(r, form.ParseOptions{MaxFieldBytes: maxMarkdownBytes}); err != nil {
		m.projectEditorError(w, r, Project{ID: r.PathValue("id")}, err.Message, err.StatusCode)
		return
	}
	p := Project{
		ID:           r.PathValue("id"),
		Slug:         strings.TrimSpace(r.PostForm.Get("slug")),
		Title:        strings.TrimSpace(r.PostForm.Get("title")),
		Summary:      strings.TrimSpace(r.PostForm.Get("summary")),
		Markdown:     r.PostForm.Get("content"),
		PreviewImage: r.PostForm.Get("previewImage"),
		Published:    r.PostForm.Get("published") == "on",
	}
	message := validatePost(Post{ID: p.ID, Slug: p.Slug, Title: p.Title, Summary: p.Summary, Markdown: p.Markdown})
	if message == "" && p.Summary == "" {
		message = "Enter a short description for the project card."
	}
	if value := strings.TrimSpace(r.PostForm.Get("displayOrder")); value != "" {
		order, err := strconv.Atoi(value)
		if err == nil && order >= 0 && order <= 1_000_000 {
			p.DisplayOrder = &order
		} else if message == "" {
			message = "Enter a display order from 0 to 1,000,000, or leave it blank."
		}
	}
	if message == "" && r.PostForm.Get("published") != "" && r.PostForm.Get("published") != "on" {
		message = "Choose whether to publish the project."
	}
	if message != "" {
		m.projectEditorError(w, r, p, message, http.StatusUnprocessableEntity)
		return
	}

	if p.PreviewImage != "" {
		image, err := m.images.Open(r.Context(), p.PreviewImage)
		if err != nil {
			if errors.Is(err, errNotFound) {
				m.projectEditorError(w, r, p, "Upload a valid preview image or remove it to use the default.", http.StatusUnprocessableEntity)
			} else {
				logger.LogRequestError(r, err)
				m.projectEditorError(w, r, p, "Unable to read the preview image. Please try again.", http.StatusInternalServerError)
			}
			return
		}
		image.Content.Close()
	}
	saved, err := m.projectStore.SaveProject(r.Context(), p)
	if errors.Is(err, errProjectSlugTaken) {
		m.projectEditorError(w, r, p, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		if errors.Is(err, errNotFound) {
			m.storageError(w, r, err)
			return
		}
		logger.LogRequestError(r, err)
		m.projectEditorError(w, r, p, "Unable to save this project. Please try again.", http.StatusInternalServerError)
		return
	}
	util.RedirectToURL(w, r, "/project/s/"+saved.Slug)
}

// projectEditorError preserves the mounted editor on HTMX errors and redisplays
// submitted values for ordinary form submissions.
func (m *module) projectEditorError(w http.ResponseWriter, r *http.Request, p Project, message string, status int) {
	page := m.views["projectEditor"].Content(r, projectPage{Project: p, Error: message})
	if page.IsHtmx() {
		page = m.views["message"].Content(r, message)
	}
	page.ResponseStatusCode = status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display the form error.", err)
	}
}

// confirmProjectDelete displays a confirmation without changing storage.
func (m *module) confirmProjectDelete(w http.ResponseWriter, r *http.Request) {
	p, err := m.projectStore.GetProjectByID(r.Context(), r.PathValue("id"))
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	page := m.views["projectDelete"].Content(r, p)
	page.Title = "Delete project"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this project.", err)
	}
}

// deleteProject removes the project but does not delete potentially shared uploads.
func (m *module) deleteProject(w http.ResponseWriter, r *http.Request) {
	if err := m.projectStore.DeleteProject(r.Context(), r.PathValue("id")); err != nil {
		m.storageError(w, r, err)
		return
	}
	util.RedirectToURL(w, r, "/projects")
}
