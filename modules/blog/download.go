package blog

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func mediaURL(id string) string {
	if validDownloadID(id) {
		return "/files/" + id
	}
	return "/images/" + id
}

// imageFiles keeps file attachments out of every existing image chooser.
func (m *module) imageFiles(ctx context.Context) ([]mediaFile, error) {
	files, err := m.media.List(ctx)
	return slices.DeleteFunc(files, func(f mediaFile) bool { return !validImageID(f.ID) }), err
}

// openMedia gives reference validation and cleanup one closeable representation.
func (m *module) openMedia(ctx context.Context, id string) (storedMedia, error) {
	if !validDownloadID(id) {
		return m.images.Open(ctx, id)
	}
	file, err := m.downloads.Open(ctx, id)
	if err != nil {
		return storedMedia{}, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return storedMedia{}, err
	}
	return storedMedia{Content: file, MediaType: downloadType(id), Modified: info.ModTime(), Digest: id[:64]}, nil
}

// download authorizes before evaluating ranges or cache validators. A saved
// historical reference retains bytes but never makes those bytes public.
func (m *module) download(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("id")
	preview := r.URL.Query().Get("preview") == "1"
	if !validDownloadID(id) || preview && downloadType(id) != "application/pdf" && downloadType(id) != "audio/mpeg" {
		m.storageError(w, r, errNotFound)
		return
	}
	permission := m.requestRole(r)
	allowed := permission == roleAdmin
	public := false
	var err error
	if !allowed {
		public, err = m.publicMedia(r.Context(), id)
		allowed = public
	}
	if err == nil && !allowed && permission == rolePoster {
		allowed, err = m.privateMediaForWriter(r.Context(), id, user.GetUserFromContext(r.Context()).ID)
	}
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	if !allowed {
		m.storageError(w, r, errNotFound)
		return
	}
	metadata, err := m.mediaMetadata.GetMediaMetadata(r.Context(), id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	file, err := m.downloads.Open(r.Context(), id)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	name := uploadFilename(metadata.Filename)
	if name == "" {
		name = id
	}
	disposition := "attachment"
	if preview {
		disposition = "inline"
	}
	if preview && downloadType(id) == "application/pdf" {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	}
	w.Header().Set("Content-Type", downloadType(id))
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", strconv.Quote(id[:64]))
	if public {
		w.Header().Set("Cache-Control", "public, no-cache")
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
}

// mediaActor preserves the JSON contract when a fetch upload or picker loses
// its session. No redirect can replace the author's unsaved form.
func (m *module) mediaActor(w http.ResponseWriter, r *http.Request) (*user.User, role, bool) {
	w.Header().Set("Cache-Control", "no-store")
	account := user.GetUserFromContext(r.Context())
	if account == nil {
		writeJSON(w, r, http.StatusUnauthorized, map[string]string{"code": "authentication_required", "error": "Sign in again to add files or link previews."})
		return nil, "", false
	}
	permission, err := m.access.Role(r.Context(), account)
	if err != nil {
		logger.LogRequestError(r, err)
		writeJSON(w, r, http.StatusServiceUnavailable, map[string]string{"error": "Unable to check your site access. Try again shortly."})
		return nil, "", false
	}
	if !permission.valid() {
		code, message := "access_denied", "This account cannot add files or link previews."
		if account.NeedsVerification() {
			code, message = "verification_required", "Verify your email before adding files or link previews."
		}
		writeJSON(w, r, http.StatusForbidden, map[string]string{"code": code, "error": message})
		return nil, "", false
	}
	return account, permission, true
}

func (m *module) uploadDownload(w http.ResponseWriter, r *http.Request) {
	account, permission, ok := m.mediaActor(w, r)
	if !ok {
		return
	}
	if !m.downloadUploadMu.TryLock() {
		writeJSON(w, r, http.StatusTooManyRequests, map[string]string{"error": "Another file is uploading. Try again shortly."})
		return
	}
	defer m.downloadUploadMu.Unlock()
	entry, status, err := m.storeDownload(w, r, account.ID, permission)
	if err != nil {
		writeJSON(w, r, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, r, http.StatusOK, entry)
}

func (m *module) storeDownload(w http.ResponseWriter, r *http.Request, accountID string, permission role) (postDownload, int, error) {
	failed := func(err error) (postDownload, int, error) {
		logger.LogRequestError(r, err)
		return postDownload{}, http.StatusInternalServerError, errors.New("Unable to save the file. Retry the upload.")
	}
	if err := r.ParseMultipartForm(1024 * 1024); err != nil {
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		status := http.StatusBadRequest
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			status = http.StatusRequestEntityTooLarge
		}
		return postDownload{}, status, errors.New("Unable to read the file upload. Choose a file of at most 32 MiB.")
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["file"]
	if len(files) != 1 || len(r.MultipartForm.File) != 1 || files[0].Size > maxDownloadBytes {
		return postDownload{}, http.StatusUnprocessableEntity, contentError("Choose one PDF, ZIP, text, or MP3 file of at most 32 MiB.")
	}
	file, err := files[0].Open()
	if err != nil {
		return failed(err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxDownloadBytes+1))
	if err != nil {
		return failed(err)
	}
	extension, err := validateDownload(data, files[0].Filename)
	if err != nil {
		return postDownload{}, http.StatusUnprocessableEntity, err
	}
	id := downloadID(data, extension)
	m.mediaMu.Lock()
	defer m.mediaMu.Unlock()
	metadata, err := m.mediaMetadata.GetMediaMetadata(r.Context(), id)
	if err != nil {
		return failed(err)
	}
	stored, err := m.downloads.Open(r.Context(), id)
	exists := err == nil
	if exists {
		if err := stored.Close(); err != nil {
			return failed(err)
		}
	} else if !errors.Is(err, errNotFound) {
		return failed(err)
	}
	allowed := permission == roleAdmin || metadata.UploadedBy == accountID || metadata.Version == 0 && !exists
	if !allowed {
		allowed, err := m.publicMedia(r.Context(), id)
		if err == nil && !allowed {
			allowed, err = m.privateMediaForWriter(r.Context(), id, accountID)
		}
		if err != nil {
			return failed(err)
		}
		if !allowed {
			return postDownload{}, http.StatusConflict, contentError("This file belongs to another account. Ask an admin to add it to your post.")
		}
	}
	size := int64(len(data))
	var storageFull bool
	if !exists {
		var err error
		storageFull, err = m.checkUploadQuota(r.Context(), id, size)
		if err != nil {
			var quota uploadQuotaError
			if errors.As(err, &quota) {
				reportFullUploadStorage(w, storageFull)
				return postDownload{}, http.StatusUnprocessableEntity, err
			}
			return failed(err)
		}
	}
	name := uploadFilename(files[0].Filename)
	if err := m.mediaMetadata.RememberMedia(r.Context(), id, mediaMetadata{Filename: name, UploadedBy: accountID, ContentType: downloadType(id), SizeBytes: &size, Checksum: id[:64]}); err != nil {
		return failed(err)
	}
	if !exists {
		if _, err := m.downloads.Put(r.Context(), data, extension); err != nil {
			return failed(err)
		}
		reportFullUploadStorage(w, storageFull)
	}
	if err := m.mediaMetadata.RecordMediaUpload(r.Context(), id, time.Now()); err != nil {
		return failed(err)
	}
	if metadata.Filename != "" {
		name = metadata.Filename
	}
	return postDownload{ID: id, Label: name, Size: size}, http.StatusOK, nil
}

func (m *module) downloadLibrary(w http.ResponseWriter, r *http.Request) {
	account, permission, ok := m.mediaActor(w, r)
	if !ok {
		return
	}
	files, err := m.media.List(r.Context())
	if err != nil {
		logger.LogRequestError(r, err)
		writeJSON(w, r, http.StatusInternalServerError, map[string]string{"error": "Unable to load files."})
		return
	}
	slices.SortFunc(files, func(a, b mediaFile) int { return b.Modified.Compare(a.Modified) })
	metadata, err := m.mediaMetadata.ListMediaMetadata(r.Context())
	var entries []postDownload
	for _, file := range files {
		if err != nil || !validDownloadID(file.ID) {
			continue
		}
		allowed := permission == roleAdmin
		if !allowed {
			allowed, err = m.privateMediaForWriter(r.Context(), file.ID, account.ID)
			if err == nil && !allowed {
				allowed, err = m.publicMedia(r.Context(), file.ID)
			}
		}
		if allowed && err == nil {
			name := metadata[file.ID].Filename
			if name == "" {
				name = file.ID
			}
			entries = append(entries, postDownload{ID: file.ID, Label: name, Size: file.Size})
		}
	}
	if err != nil {
		logger.LogRequestError(r, err)
		writeJSON(w, r, http.StatusInternalServerError, map[string]string{"error": "Unable to load files."})
		return
	}
	writeJSON(w, r, http.StatusOK, entries)
}
