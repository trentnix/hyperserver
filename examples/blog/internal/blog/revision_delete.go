package blog

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/util"
)

// confirmRevisionDelete identifies an older revision before its permanent removal.
func (m *module) confirmRevisionDelete(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	number, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	if err != nil || number < 1 {
		m.storageError(w, r, errNotFound)
		return
	}
	selected, err := m.history.GetRevision(r.Context(), kind, id, number)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	versions, err := m.history.ListRevisions(r.Context(), kind, id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	if len(versions) == 0 || versions[0].ID == number {
		m.app.ContentManager.HandleError(w, r, "The current version cannot be deleted from history.", nil, http.StatusConflict)
		return
	}

	data := historyPage{URL: "/history/" + kind + "/" + id, Selected: selected}
	page := m.views["revisionDelete"].Content(r, data)
	page.Title = "Delete revision"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this revision.", err)
	}
}

// deleteRevision removes only history, then selects an older version when available.
func (m *module) deleteRevision(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	number, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	if err != nil || number < 1 {
		m.storageError(w, r, errNotFound)
		return
	}

	var contentURL string
	switch kind {
	case "post":
		p, err := m.posts.GetByID(r.Context(), id, true)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		contentURL = "/post/s/" + p.Slug
	case "page":
		p, err := m.pageStore.GetPageByID(r.Context(), id, true)
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		contentURL = p.URL()
	default:
		m.storageError(w, r, errNotFound)
		return
	}
	err = m.history.DeleteRevision(r.Context(), kind, id, number)
	if errors.Is(err, errCurrentRevision) {
		m.app.ContentManager.HandleError(w, r, "The current version cannot be deleted from history.", nil, http.StatusConflict)
		return
	}
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	versions, err := m.history.ListRevisions(r.Context(), kind, id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	if len(versions) > 1 {
		selected := versions[len(versions)-1].ID
		for _, version := range versions {
			if version.ID < number {
				selected = version.ID
				break
			}
		}
		contentURL = "/history/" + kind + "/" + id + "?revision=" + strconv.FormatInt(selected, 10)
		if request := htmx.GetRequest(r); request.Enabled && !request.HistoryRestore {
			// Let the existing history page remove the entry and load its next preview.
			payload, _ := json.Marshal(map[string]map[string]string{
				"blog:revision-deleted": {"revision": strconv.FormatInt(number, 10), "next": contentURL},
			})
			response := htmx.Response{NoContent: true, Trigger: string(payload)}
			response.Apply(w)
			return
		}
	}
	util.RedirectToURL(w, r, contentURL)
}
