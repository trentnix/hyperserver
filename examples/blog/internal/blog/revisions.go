package blog

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
)

var errEditConflict = errors.New("content changed since the editor was opened")
var errCurrentRevision = errors.New("the current revision cannot be deleted")

const editConflictMessage = "A newer version was saved while this editor was open. Your edits have not been saved. Open the current editor in another tab to compare before trying again."

// Revision is an immutable content snapshot. Publication, URLs, and page placement
// are deliberately excluded so loading old writing cannot change those settings.
type Revision struct {
	ID                                     int64
	Title, Summary, Markdown, PreviewImage string
	RecordedAt                             time.Time
}

// revisionRepository reads and removes saved versions. Content repositories record them atomically on save.
type revisionRepository interface {
	ListRevisions(context.Context, string, string) ([]Revision, error)
	GetRevision(context.Context, string, string, int64) (Revision, error)
	// DeleteRevision must atomically protect the latest snapshot and leave current content unchanged.
	DeleteRevision(context.Context, string, string, int64) error
	HasHistory(context.Context, string, string) (bool, error)
	HistoryOwners(context.Context, string) (map[string]bool, error)
}

// revisionSelection supplies the editor notice and the current content used by
// browser recovery to recognize a loaded version as unsaved work.
type revisionSelection struct {
	LoadedRevision *Revision
	CurrentContent string
	RevisionID     int64
	HistoryURL     string
	MissingImages  bool
}

// editorRevision keeps the selected history location through ordinary failed
// submissions. It builds only a local URL and never replaces submitted writing.
func editorRevision(r *http.Request, kind, id string) revisionSelection {
	number, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if err != nil || number < 1 || id == "" {
		return revisionSelection{}
	}
	return revisionSelection{
		RevisionID: number,
		HistoryURL: "/history/" + kind + "/" + url.PathEscape(id) + "?revision=" + strconv.FormatInt(number, 10),
	}
}

// selectRevision reads only a version belonging to this content. Empty selection
// opens the ordinary editor. The JSON is escaped by html/template in an attribute.
func (m *module) selectRevision(r *http.Request, kind, id, contentPath string, current Revision) (revisionSelection, error) {
	raw := r.URL.Query().Get("revision")
	if raw == "" {
		return revisionSelection{}, nil
	}
	selection := editorRevision(r, kind, id)
	if selection.RevisionID == 0 {
		return revisionSelection{}, errNotFound
	}
	selected, err := m.history.GetRevision(r.Context(), kind, id, selection.RevisionID)
	if err != nil {
		return revisionSelection{}, err
	}
	missing, err := m.revisionMissingImages(r.Context(), selected, contentPath)
	if err != nil {
		return revisionSelection{}, err
	}
	selection.MissingImages = len(missing) > 0
	baseline, err := json.Marshal(map[string]string{
		"title": current.Title, "summary": current.Summary,
		"content": current.Markdown, "previewImage": current.PreviewImage,
	})
	selection.LoadedRevision, selection.CurrentContent = &selected, string(baseline)
	return selection, err
}

// historyPage describes saved versions and their preview, not an editor.
type historyPage struct {
	Title, URL, EditorURL         string
	ContentURL, Kind              string
	Versions                      []Revision
	Selected                      Revision
	Current                       bool
	HTML                          template.HTML
	MissingImages, MissingPreview bool
}

// revisions shows administrator-only history without modifying content or publication.
func (m *module) revisions(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	data := historyPage{URL: "/history/" + kind + "/" + id, Kind: kind}
	var contentPath string
	switch kind {
	case "post":
		p, err := m.posts.GetByID(r.Context(), id, true)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		data.Title, data.EditorURL = p.Title, "/post/update/"+id
		contentPath = "/post/s/" + p.Slug
	case "page":
		p, err := m.pageStore.GetPageByID(r.Context(), id, true)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		data.Title, data.EditorURL = p.Title, "/pages/update/"+id
		contentPath = p.URL()
	default:
		m.storageError(w, r, errNotFound)
		return
	}
	versions, err := m.history.ListRevisions(r.Context(), kind, id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	if len(versions) == 0 {
		m.storageError(w, r, errNotFound)
		return
	}
	selected := versions[0].ID
	if raw := r.URL.Query().Get("revision"); raw != "" {
		selected, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || selected < 1 {
			m.storageError(w, r, errNotFound)
			return
		}
	}
	data.Selected, err = m.history.GetRevision(r.Context(), kind, id, selected)
	if err == nil {
		data.HTML, err = markdownAt(data.Selected.Markdown, &url.URL{Path: contentPath})
	}
	if err == nil {
		var missing map[string]bool
		missing, err = m.revisionMissingImages(r.Context(), data.Selected, contentPath)
		if err == nil {
			data.MissingImages = len(missing) > 0
			data.MissingPreview = missing["/images/"+data.Selected.PreviewImage]
			data.HTML, err = markMissingImages(data.HTML, missing)
		}
	}
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	data.Versions, data.Current = versions, selected == versions[0].ID
	data.ContentURL = contentPath
	request := htmx.GetRequest(r)
	view := "history"
	if request.Enabled && !request.HistoryRestore && request.Target == "revision-preview" {
		view = "revisionPreview"
	}
	page := m.views[view].Content(r, data)
	page.Headers = map[string]string{"Vary": htmx.HeaderTarget}
	if view == "revisionPreview" {
		page.HTMX.Response = &htmx.Response{ReplaceURL: data.URL + "?revision=" + strconv.FormatInt(selected, 10)}
	}
	page.Title = "History — " + data.Title
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display saved versions.", err)
	}
}

// editorVersion validates the hidden concurrency token. New content has no prior version.
func editorVersion(r *http.Request) (int64, bool) {
	if r.PathValue("id") == "" {
		return 0, true
	}
	version, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	return version, err == nil && version > 0
}
