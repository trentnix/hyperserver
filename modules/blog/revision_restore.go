package blog

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/util"
)

// restoreRevision applies only stored writing, never submitted content or historical settings.
func (m *module) restoreRevision(w http.ResponseWriter, r *http.Request) {
	if parseError := form.Parse(r); parseError != nil {
		m.app.ContentManager.HandleError(w, r, parseError.Message, nil, parseError.StatusCode)
		return
	}
	kind, id := r.PathValue("kind"), r.PathValue("id")
	number, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if err != nil || number < 1 {
		m.storageError(w, r, errNotFound)
		return
	}
	selected, err := m.history.GetRevision(r.Context(), kind, id, number)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	version, valid := editorVersion(r)
	if !valid {
		m.renderRevisions(w, r, "The content version is missing or invalid. Reload History before restoring.", http.StatusBadRequest)
		return
	}
	err = m.restoreRevisionContent(r.Context(), kind, id, version, selected)
	var invalid contentError
	switch {
	case errors.Is(err, errAccessDenied):
		m.app.ContentManager.HandleError(w, r, err.Error(), nil, http.StatusForbidden)
	case errors.Is(err, errEditConflict):
		m.renderRevisions(w, r, "The content changed since you opened History. Review the latest version before restoring.", http.StatusConflict)
	case errors.Is(err, errMissingMedia):
		m.renderRevisions(w, r, "This version references deleted images. Edit this version to replace or remove them before saving.", http.StatusUnprocessableEntity)
	case errors.As(err, &invalid):
		m.renderRevisions(w, r, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, errNotFound):
		m.storageError(w, r, err)
	case err != nil:
		logger.LogRequestError(r, err)
		m.renderRevisions(w, r, "Unable to restore this version. Please try again.", http.StatusInternalServerError)
	default:
		util.RedirectToURL(w, r, "/history/"+kind+"/"+id)
	}
}

// restoreRevisionContent preserves current settings and checks media under the same lock as ordinary saves.
// Repository version checks prevent intervening edits from being overwritten.
func (m *module) restoreRevisionContent(ctx context.Context, kind, id string, version int64, selected Revision) error {
	var current Revision
	var currentVersion int64
	var save func() error
	switch kind {
	case "post":
		p, err := m.posts.GetByID(ctx, id, true)
		if err != nil {
			return err
		}
		p, err = m.authorizePostWrite(ctx, p)
		if err != nil {
			return err
		}
		current = Revision{Title: p.Title, Summary: p.Summary, Markdown: p.Markdown, PreviewImage: p.PreviewImage, Tags: p.Tags}
		currentVersion = p.Version
		p.Title, p.Summary, p.Markdown, p.PreviewImage, p.Tags = selected.Title, selected.Summary, selected.Markdown, selected.PreviewImage, selected.Tags
		save = func() error {
			_, err := m.persistPost(ctx, p)
			return err
		}
	case "page":
		if err := m.authorizeAdminWrite(ctx); err != nil {
			return err
		}
		p, err := m.pageStore.GetPageByID(ctx, id, true)
		if err != nil {
			return err
		}
		current = Revision{Title: p.Title, Summary: p.Summary, Markdown: p.Markdown, PreviewImage: p.PreviewImage, Tags: p.Tags}
		currentVersion = p.Version
		p.Title, p.Summary, p.Markdown, p.PreviewImage, p.Tags = selected.Title, selected.Summary, selected.Markdown, selected.PreviewImage, selected.Tags
		save = func() error {
			_, err := m.persistPage(ctx, p)
			return err
		}
	default:
		return errNotFound
	}
	if currentVersion != version {
		return errEditConflict
	}
	if selected.sameContent(current) {
		return nil
	}
	return save()
}
