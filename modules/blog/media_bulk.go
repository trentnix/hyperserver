package blog

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// maxMediaDeleteBatch keeps the confirmation within the route's request-body limit.
const maxMediaDeleteBatch = 5000

// mediaDeleteBatch carries the exact uploads reviewed on the confirmation page.
type mediaDeleteBatch struct {
	Files     []mediaFile
	Bytes     int64
	Remaining int
}

// confirmUnusedMediaDelete snapshots candidates without deleting or reserving them.
func (m *module) confirmUnusedMediaDelete(w http.ResponseWriter, r *http.Request) {
	files, err := m.unusedImages(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	data := mediaDeleteBatch{Files: files}
	if len(files) > maxMediaDeleteBatch {
		data.Files = files[:maxMediaDeleteBatch]
		data.Remaining = len(files) - maxMediaDeleteBatch
	}
	for _, file := range data.Files {
		data.Bytes += file.Size
	}
	page := m.views["mediaDeleteAll"].Content(r, data)
	page.Title = "Delete unused images"
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display unused images.", err)
	}
}

// mediaDeleteResult reports partial progress accurately if storage fails mid-batch.
type mediaDeleteResult struct {
	Deleted, Skipped, Remaining int
	Failed                      bool
}

// deleteUnusedMedia validates the entire submitted list before removing any file.
// Replaying the form cannot add uploads that appeared after confirmation.
func (m *module) deleteUnusedMedia(w http.ResponseWriter, r *http.Request) {
	if err := form.Parse(r); err != nil {
		m.renderMedia(w, r, err.Message, err.StatusCode)
		return
	}
	files, err := parseMediaDeleteBatch(r.PostForm["file"])
	if err != nil {
		m.renderMedia(w, r, "Reload the cleanup page and review the images before deleting.", http.StatusBadRequest)
		return
	}
	result, err := m.removeUnusedMediaBatch(r.Context(), files)
	status := http.StatusOK
	if err != nil {
		logger.LogRequestError(r, err)
		status = http.StatusInternalServerError
		result.Failed = true
	}
	page := m.views["mediaDeleteResult"].Content(r, result)
	page.Title, page.ResponseStatusCode = "Media cleanup result", status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display the cleanup result.", err)
	}
}

// parseMediaDeleteBatch rejects malformed or duplicate entries before destructive work.
func parseMediaDeleteBatch(values []string) ([]mediaFile, error) {
	if len(values) == 0 || len(values) > maxMediaDeleteBatch {
		return nil, errors.New("invalid media batch size")
	}
	files := make([]mediaFile, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		id, rawVersion, ok := strings.Cut(value, ":")
		version, err := strconv.ParseInt(rawVersion, 10, 64)
		if !ok || !validImageID(id) || err != nil || seen[id] {
			return nil, errors.New("invalid media batch entry")
		}
		seen[id] = true
		files = append(files, mediaFile{ID: id, Modified: time.Unix(0, version)})
	}
	return files, nil
}

// removeUnusedMediaBatch checks references once while excluding saves and uploads.
// Changed, referenced, or already removed files are skipped. Other errors stop the batch.
func (m *module) removeUnusedMediaBatch(ctx context.Context, files []mediaFile) (mediaDeleteResult, error) {
	m.mediaMu.Lock()
	defer m.mediaMu.Unlock()
	result := mediaDeleteResult{Remaining: len(files)}
	used, err := m.mediaReferences.ReferencedImages(ctx)
	if err != nil {
		return result, err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if usage := used[file.ID]; usage.Current || len(usage.History) > 0 {
			result.Skipped++
		} else {
			err := m.media.Remove(ctx, file.ID, file.Version())
			switch {
			case err == nil:
				result.Deleted++
			case errors.Is(err, errMediaChanged), errors.Is(err, errNotFound):
				result.Skipped++
			default:
				return result, err
			}
		}
		result.Remaining--
	}
	return result, nil
}
