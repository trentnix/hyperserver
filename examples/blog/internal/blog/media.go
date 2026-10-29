package blog

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/util"
)

// mediaPage separates unused uploads from those retained only by history.
type mediaPage struct {
	Files   []mediaFile
	History []historicalMedia
	Message string
}

// historicalMedia requires an individual confirmation, never bulk cleanup.
type historicalMedia struct {
	mediaFile
	History []mediaRevision
}

// unusedMedia lists candidates without deleting anything or changing image visibility.
func (m *module) unusedMedia(w http.ResponseWriter, r *http.Request) {
	m.renderMedia(w, r, "", http.StatusOK)
}

// renderMedia recomputes candidates after an action so stale links never imply safety.
func (m *module) renderMedia(w http.ResponseWriter, r *http.Request, message string, status int) {
	data, err := m.mediaCandidates(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	data.Message = message
	page := m.views["media"].Content(r, data)
	page.Title, page.ResponseStatusCode = "Media cleanup", status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display unused media.", err)
	}
}

// unusedImages supplies candidates for listing or confirmation, not deletion authority.
func (m *module) unusedImages(ctx context.Context) ([]mediaFile, error) {
	data, err := m.mediaCandidates(ctx)
	return data.Files, err
}

// mediaCandidates separates unreferenced files from files used only by history.
func (m *module) mediaCandidates(ctx context.Context) (mediaPage, error) {
	used, err := m.mediaReferences.ReferencedImages(ctx)
	if err != nil {
		return mediaPage{}, err
	}
	files, err := m.media.List(ctx)
	if err != nil {
		return mediaPage{}, err
	}
	var data mediaPage
	for _, file := range files {
		usage := used[file.ID]
		if usage.Current {
			continue
		}
		if len(usage.History) > 0 {
			data.History = append(data.History, historicalMedia{mediaFile: file, History: usage.History})
		} else {
			data.Files = append(data.Files, file)
		}
	}
	return data, nil
}

// confirmMediaDelete shows the image and requires an explicit destructive submission.
func (m *module) confirmMediaDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	image, err := m.images.Open(r.Context(), id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	image.Content.Close()
	used, err := m.mediaReferences.ReferencedImages(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	if used[id].Current {
		m.renderMedia(w, r, "This image is now used by saved content and cannot be deleted.", http.StatusConflict)
		return
	}
	page := m.views["mediaDelete"].Content(r, historicalMedia{mediaFile: mediaFile{ID: id, Modified: image.Modified}, History: used[id].History})
	page.Title = "Delete image"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display this image.", err)
	}
}

// deleteMedia rechecks references while saves and uploads are excluded. A failed
// check never deletes a file. Removal affects only the selected upload, not backups.
func (m *module) deleteMedia(w http.ResponseWriter, r *http.Request) {
	if err := form.Parse(r); err != nil {
		m.renderMedia(w, r, err.Message, err.StatusCode)
		return
	}
	version, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	if err != nil {
		m.renderMedia(w, r, "Reload the cleanup page before deleting an image.", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	err = m.removeMedia(r.Context(), id, version, r.PostForm.Get("deleteHistory") == "true")
	switch {
	case errors.Is(err, errMediaUsed):
		m.renderMedia(w, r, "This image is now used by saved content and cannot be deleted.", http.StatusConflict)
	case errors.Is(err, errMediaHistory):
		m.renderMedia(w, r, "This image is used by saved versions. Review its history references and confirm deletion.", http.StatusConflict)
	case errors.Is(err, errMediaChanged):
		m.renderMedia(w, r, "This image was uploaded again. Review it before deleting.", http.StatusConflict)
	case err != nil:
		m.storageError(w, r, err)
	default:
		util.RedirectToURL(w, r, "/media/unused")
	}
}

var errMediaUsed = errors.New("image is referenced by saved content")
var errMediaHistory = errors.New("image is referenced by saved versions")

// removeMedia owns the check-and-remove boundary. Current content is always protected.
func (m *module) removeMedia(ctx context.Context, id string, version int64, deleteHistory bool) error {
	m.mediaMu.Lock()
	defer m.mediaMu.Unlock()
	used, err := m.mediaReferences.ReferencedImages(ctx)
	if err != nil {
		return err
	}
	if used[id].Current {
		return errMediaUsed
	}
	if len(used[id].History) > 0 && !deleteHistory {
		return errMediaHistory
	}
	return m.media.Remove(ctx, id, version)
}

var errMissingMedia = errors.New("An uploaded image is no longer available. Upload it again or remove its reference before saving.")

// checkMediaReferences rejects stale editor references after cleanup. The caller
// must hold mediaMu until the content is saved. External images are not fetched.
func (m *module) checkMediaReferences(ctx context.Context, references []string) error {
	missing, err := m.missingMedia(ctx, references)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return errMissingMedia
	}
	return nil
}

// missingMedia checks local upload references without fetching external images.
// Storage failures are errors, not evidence that an image was deleted.
func (m *module) missingMedia(ctx context.Context, references []string) (map[string]bool, error) {
	missing := make(map[string]bool)
	// Match the configured listener when publicOrigin is absent, including its
	// default HTTPS port. Profile saves need no incoming request to establish it.
	request := &http.Request{}
	if m.app.Config.HTTP.TLS.Enabled {
		request.TLS = &tls.ConnectionState{}
	}
	for _, reference := range references {
		id := referencedImageID(reference)
		if id == "" {
			continue
		}
		normalized := imageReference(reference, "/")
		local := m.imageLookupReferences(request, id)
		if !slices.Contains(local, normalized) {
			continue
		}
		image, err := m.images.Open(ctx, id)
		if errors.Is(err, errNotFound) {
			missing[normalized] = true
			continue
		}
		if err != nil {
			return nil, err
		}
		image.Content.Close()
	}
	return missing, ctx.Err()
}

// checkContentImages validates rendered Markdown references without changing the source.
func (m *module) checkContentImages(r *http.Request, source, contentPath string) error {
	refs, err := imageReferences(source, contentPath, "")
	if err != nil {
		return err
	}
	return m.checkMediaReferences(r.Context(), refs)
}
