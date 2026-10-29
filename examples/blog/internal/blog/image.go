package blog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/trentnix/hyperserver/pkg/imaging"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

const maxImageBytes = 8 * 1024 * 1024

// imageUpload groups the original and generated copies stored as one upload.
type imageUpload = imaging.Result

// prepareImage applies blog limits and translates processing failures into editor messages.
func prepareImage(ctx context.Context, data []byte) (imageUpload, error) {
	upload, err := imaging.Process(ctx, data, imaging.Options{
		MaxBytes: maxImageBytes, MaxPixels: 20_000_000,
		DisplaySize: 2048, ThumbnailSize: 480, JPEGQuality: 85,
	})
	switch {
	case errors.Is(err, imaging.ErrByteLimit):
		err = errors.New("Choose an image of at most 8 MiB.")
	case errors.Is(err, imaging.ErrPixelLimit):
		err = errors.New("Images must contain at most 20 million pixels.")
	case errors.Is(err, imaging.ErrFormat):
		err = errors.New("Choose a valid PNG, JPEG, or GIF.")
	case errors.Is(err, imaging.ErrInvalidImage):
		err = errors.New("The image is incomplete or damaged.")
	case errors.Is(err, imaging.ErrOrientation):
		err = errors.New("Unable to read image orientation. Export the image without metadata and try again.")
	case errors.Is(err, imaging.ErrAnimation):
		err = errors.New("Animated PNGs are not supported. Use an animated GIF instead.")
	}
	return upload, err
}

// upload accepts EasyMDE's image field and returns its documented JSON response.
// Both compressed size and decoded dimensions are bounded before decoding pixels.
func (m *module) upload(w http.ResponseWriter, r *http.Request) {
	select {
	case m.imageProcessing <- struct{}{}:
		defer func() { <-m.imageProcessing }()
	default:
		w.Header().Set("Retry-After", "1")
		writeJSON(w, r, http.StatusTooManyRequests, map[string]string{"error": "Another image is being processed. Try again shortly."})
		return
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
		writeJSON(w, r, status, map[string]string{"error": "Unable to read the image upload."})
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["image"]
	if len(files) != 1 || len(r.MultipartForm.File) != 1 || files[0].Size > maxImageBytes {
		writeJSON(w, r, http.StatusUnprocessableEntity, map[string]string{"error": "Choose one PNG, JPEG, or GIF of at most 8 MiB."})
		return
	}
	file, err := files[0].Open()
	if err != nil {
		writeJSON(w, r, http.StatusBadRequest, map[string]string{"error": "Unable to open the image."})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
	if err != nil || len(data) > maxImageBytes {
		writeJSON(w, r, http.StatusUnprocessableEntity, map[string]string{"error": "Unable to read the image, or it exceeds 8 MiB."})
		return
	}
	upload, err := prepareImage(r.Context(), data)
	if err != nil {
		writeJSON(w, r, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	m.mediaMu.RLock()
	defer m.mediaMu.RUnlock()
	id, err := m.images.Put(r.Context(), upload)
	if err != nil {
		logger.LogRequestError(r, err)
		writeJSON(w, r, http.StatusInternalServerError, map[string]string{"error": "Unable to save the image."})
		return
	}
	// EasyMDE's upload contract requires 200, not 201.
	writeJSON(w, r, http.StatusOK, map[string]any{"data": map[string]string{"filePath": "/images/" + id}})
}

// image checks visibility before serving bytes or conditional responses. Private
// uploads are administrator-only. Public responses must revalidate their access.
func (m *module) image(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	admin := m.access.IsAdmin(user.GetUserFromContext(r.Context()))
	variant := imageVariant(r.URL.Query().Get("variant"))
	if variant == "" {
		variant = displayImage
	}
	if (variant != displayImage && variant != thumbnailImage && variant != originalImage) || (variant == originalImage && !admin) {
		m.storageError(w, r, errNotFound)
		return
	}
	if !admin {
		public, err := m.publicImage(r, r.PathValue("id"))
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		if !public {
			m.storageError(w, r, errNotFound)
			return
		}
	}
	stored, err := m.images.OpenVariant(r.Context(), r.PathValue("id"), variant)
	if err != nil {
		if errors.Is(err, errImageNotOptimized) {
			err = errNotFound
		}
		m.storageError(w, r, err)
		return
	}
	defer stored.Content.Close()
	w.Header().Set("Content-Type", stored.MediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if variant == originalImage {
		w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("id")+`"`)
	}
	if !admin {
		w.Header().Set("Cache-Control", "public, no-cache")
	}
	w.Header().Set("ETag", strconv.Quote(stored.Digest))
	// Regeneration preserves the upload date, not the generated bytes. Validate
	// generated copies by digest only so an old date cannot hide a new copy.
	var modified time.Time
	if variant == originalImage {
		modified = stored.Modified
	}
	http.ServeContent(w, r, "", modified, stored.Content)
}

// writeJSON sends an editor response and logs encoding or write failures.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.LogRequestError(r, err)
	}
}
