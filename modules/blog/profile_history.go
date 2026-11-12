package blog

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/util"
)

// ProfileRevision preserves public identity and image URLs, never administrator configuration.
type ProfileRevision struct {
	Profile
	ID         int64
	RecordedAt time.Time
}

// Title supplies the label used by the shared saved-version list.
func (p ProfileRevision) Title() string { return p.Name }

// profileHistoryPage reuses the saved-version navigation without treating a profile as Markdown.
type profileHistoryPage struct {
	URL            string
	Versions       []ProfileRevision
	Selected       ProfileRevision
	Current        bool
	MatchesCurrent bool
	Images         []profileHistoryImage
	Version        int64
}

// profileHistoryImage shows deleted uploads explicitly instead of broken previews.
type profileHistoryImage struct {
	Label, URL string
	Missing    bool
}

// profileHistory previews saved public identity without publishing it or changing configuration.
func (m *module) profileHistory(w http.ResponseWriter, r *http.Request) {
	// Capture the edit token before reading history so an intervening save rejects a stale restore.
	current := m.profile.Current()
	versions, err := m.profileStore.ListProfileRevisions(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	if len(versions) == 0 {
		m.storageError(w, r, errNotFound)
		return
	}
	number := versions[0].ID
	if raw := r.URL.Query().Get("revision"); raw != "" {
		number, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || number < 1 {
			m.storageError(w, r, errNotFound)
			return
		}
	}
	selected, err := m.profileStore.GetProfileRevision(r.Context(), number)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	missing, err := m.missingMedia(r.Context(), []string{selected.ImageURL, selected.FaviconURL, selected.BackgroundURL})
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	data := profileHistoryPage{URL: "/profile/history", Versions: versions, Selected: selected, Current: number == versions[0].ID, Version: current.Version, MatchesCurrent: selected.Profile.sameContent(current)}
	for _, field := range (profileEditorData{Profile: selected.Profile}).ImageFields() {
		data.Images = append(data.Images, profileHistoryImage{Label: field.Label, URL: field.PreviewURL(), Missing: missing[mediaReferenceURL(field.Value, "/")]})
	}
	view := "profileHistory"
	request := htmx.GetRequest(r)
	if request.Enabled && !request.HistoryRestore && !request.FullPage && request.Target == "revision-preview" {
		view = "profileRevisionPreview"
	}
	page := m.views[view].Content(r, data)
	page.Title = "Site Profile History"
	page.Headers = map[string]string{"Vary": htmx.HeaderTarget}
	if view == "profileRevisionPreview" {
		page.HTMX.Response = &htmx.Response{ReplaceURL: data.URL + "?revision=" + strconv.FormatInt(number, 10), Retarget: "#revision-preview", Reswap: "innerHTML show:none"}
		if r.Method == http.MethodPost {
			page.OutOfBand = []string{"blog.versionListUpdate", "blog.closeDialog"}
		}
	}
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display saved profile versions.", err)
	}
}

// restoreProfile saves stored revision content against the version the administrator reviewed.
// It never trusts submitted content or changes the selected historical snapshot.
func (m *module) restoreProfile(w http.ResponseWriter, r *http.Request) {
	if parseError := form.Parse(r); parseError != nil {
		m.app.ContentManager.HandleError(w, r, parseError.Message, nil, parseError.StatusCode)
		return
	}
	number, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if err != nil || number < 1 {
		m.storageError(w, r, errNotFound)
		return
	}
	selected, err := m.profileStore.GetProfileRevision(r.Context(), number)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	version, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	if err != nil || version < 1 {
		m.renderProfileEditor(w, r, selected.Profile, "The profile version is missing or invalid. Reload the editor before saving.", http.StatusBadRequest)
		return
	}
	selected.Version = version
	m.saveProfileContent(w, r, selected.Profile, true)
}

// confirmProfileRevisionDelete previews the older snapshot before permanent deletion.
func (m *module) confirmProfileRevisionDelete(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	if err != nil || number < 1 {
		m.storageError(w, r, errNotFound)
		return
	}
	selected, err := m.profileStore.GetProfileRevision(r.Context(), number)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	versions, err := m.profileStore.ListProfileRevisions(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	if len(versions) == 0 || versions[0].ID == number {
		m.app.ContentManager.HandleError(w, r, "The current version cannot be deleted from history.", nil, http.StatusConflict)
		return
	}
	data := historyPage{URL: "/profile/history", Selected: Revision{ID: selected.ID, Title: selected.Name, RecordedAt: selected.RecordedAt}}
	page := m.views["revisionDelete"].Content(r, data)
	page.Title = "Delete profile revision"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this revision.", err)
	}
}

// deleteProfileRevision removes only history and keeps the remaining preview selected in place.
func (m *module) deleteProfileRevision(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	if err != nil || number < 1 {
		m.storageError(w, r, errNotFound)
		return
	}
	err = m.profileStore.DeleteProfileRevision(r.Context(), number)
	if errors.Is(err, errCurrentRevision) {
		m.app.ContentManager.HandleError(w, r, "The current version cannot be deleted from history.", nil, http.StatusConflict)
		return
	}
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	versions, err := m.profileStore.ListProfileRevisions(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	next := "/profile"
	if len(versions) > 1 {
		selected := versions[len(versions)-1].ID
		for _, version := range versions {
			if version.ID < number {
				selected = version.ID
				break
			}
		}
		next = "/profile/history?revision=" + strconv.FormatInt(selected, 10)
		if m.revisionDeleted(w, r, next) {
			return
		}
	}
	util.RedirectToURL(w, r, next)
}
