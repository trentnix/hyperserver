package blog

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

const maxImageBytes = 8 * 1024 * 1024

// upload accepts EasyMDE's image field and returns its documented JSON response.
// Both compressed size and decoded dimensions are bounded before decoding pixels.
func (m *module) upload(w http.ResponseWriter, r *http.Request) {
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
	media, err := imageMedia(data)
	if err != nil {
		writeJSON(w, r, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	id, err := m.images.Put(r.Context(), media, data)
	if err != nil {
		logger.LogRequestError(r, err)
		writeJSON(w, r, http.StatusInternalServerError, map[string]string{"error": "Unable to save the image."})
		return
	}
	// EasyMDE's upload contract requires 200, not 201.
	writeJSON(w, r, http.StatusOK, map[string]any{"data": map[string]string{"filePath": "/images/" + id}})
}

// imageMedia checks the format and pixel limit before decoding the image.
// For GIFs, decoding validates the first frame, not the entire animation.
func imageMedia(data []byte) (string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif") {
		return "", errors.New("Choose a valid PNG, JPEG, or GIF.")
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 20_000_000 {
		return "", errors.New("Images must contain at most 20 million pixels.")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", errors.New("The image is incomplete or damaged.")
	}
	return "image/" + format, nil
}

// image serves immutable public uploads with browser caching and standard
// conditional, HEAD, and range responses. Failed lookups must not be cached.
func (m *module) image(w http.ResponseWriter, r *http.Request) {
	stored, err := m.images.Open(r.Context(), r.PathValue("id"))
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		m.storageError(w, r, err)
		return
	}
	defer stored.Content.Close()
	w.Header().Set("Content-Type", stored.MediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", strconv.Quote(stored.Digest))
	http.ServeContent(w, r, "", stored.Modified, stored.Content)
}

// writeJSON sends an editor response and logs encoding or write failures.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.LogRequestError(r, err)
	}
}
