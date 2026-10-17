package blog

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

func (m *module) uploadForm(w http.ResponseWriter, r *http.Request) {
	m.render(w, r, "upload", "Upload an image", nil)
}

// upload validates one raster image and publishes its public URL only after Save
// succeeds. Images belong to the blog, not to an individual post or editor session.
func (m *module) upload(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(MaxImageBytes)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err != nil {
		var limit *http.MaxBytesError
		status := http.StatusBadRequest
		if errors.As(err, &limit) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "Unable to read image upload", status)
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) != 1 || len(r.MultipartForm.File) != 1 {
		http.Error(w, "Upload one PNG, JPEG, or GIF image", http.StatusBadRequest)
		return
	}
	file, err := files[0].Open()
	if err != nil {
		m.failure(w, r, err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxImageBytes+1))
	if err != nil {
		m.failure(w, r, err)
		return
	}
	if len(data) > MaxImageBytes {
		http.Error(w, "Image exceeds 5 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	// Bound decoded dimensions before allocating pixels. Division avoids overflow
	// when checking dimensions supplied by an untrusted file.
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 16000000/config.Height {
		http.Error(w, "Use a PNG, JPEG, or GIF image with at most 16 million pixels", http.StatusBadRequest)
		return
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		http.Error(w, "Image data is incomplete or invalid", http.StatusBadRequest)
		return
	}
	mediaType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif"}[format]
	if mediaType == "" {
		http.Error(w, "Unsupported image format", http.StatusBadRequest)
		return
	}
	img := Image{ID: uuid.NewString(), MediaType: mediaType, Data: data}
	if err := m.options.Images.Save(r.Context(), img); err != nil {
		m.failure(w, r, err)
		return
	}
	m.render(w, r, "upload", "Image uploaded", "/images/"+img.ID)
}

// image serves immutable public uploads with range and HEAD support. Fixed media
// types, nosniff, and a restrictive CSP keep uploads from acting as site documents.
func (m *module) image(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return
	}
	img, err := m.options.Images.Get(r.Context(), id)
	if err != nil {
		m.failure(w, r, err)
		return
	}
	if len(img.Data) == 0 || len(img.Data) > MaxImageBytes ||
		(img.MediaType != "image/png" && img.MediaType != "image/jpeg" && img.MediaType != "image/gif") {
		http.Error(w, "Invalid stored image", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", img.MediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, id, time.Time{}, bytes.NewReader(img.Data))
}
