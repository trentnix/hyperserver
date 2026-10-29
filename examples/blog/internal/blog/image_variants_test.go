package blog

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestImageMalformedBundle rejects incomplete bundles and unexpected encodings.
func TestImageMalformedBundle(t *testing.T) {
	for _, test := range []string{"missing", "duplicate", "unknown", "compressed", "digest", "empty"} {
		t.Run(test, func(t *testing.T) {
			s := newTestImageStore(t)
			var data bytes.Buffer
			archive := zip.NewWriter(&data)
			for i, name := range []string{"original", "display", "thumbnail"} {
				if test == "missing" && i == 2 {
					continue
				}
				if test == "duplicate" && i == 2 {
					name = "original"
				}
				if test == "unknown" && i == 2 {
					name = "other"
				}
				header := &zip.FileHeader{Name: name, Comment: strings.Repeat("a", 64), Method: zip.Store}
				if test == "digest" {
					header.Comment = "bad"
				}
				if test == "compressed" {
					header.Method = zip.Deflate
				}
				entry, err := archive.CreateHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				if test != "empty" {
					if _, err := entry.Write([]byte("image")); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			id := strings.Repeat("a", 64) + ".png"
			if err := os.WriteFile(filepath.Join(s.root.Name(), id), data.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if file, err := s.Open(t.Context(), id); err == nil {
				file.Content.Close()
				t.Fatal("invalid bundle served")
			}
		})
	}
}

// TestImageVariantAccess checks that variants cannot bypass publication or admin access.
func TestImageVariantAccess(t *testing.T) {
	h := newHarness(t)
	upload := imageUpload{MediaType: "image/png", Original: []byte("original-private"), Display: []byte("display-public"), Thumbnail: []byte("thumbnail-public")}
	id, err := h.blog.images.Put(t.Context(), upload)
	if err != nil {
		t.Fatal(err)
	}
	post, err := h.blog.posts.Save(t.Context(), Post{Title: "Image", Slug: "image", PreviewImage: id, Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	request := func(variant string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/images/"+id+"?variant="+url.QueryEscape(variant), nil)
		for key, value := range headers {
			r.Header.Set(key, value)
		}
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		return w
	}
	for _, variant := range []string{"display", "thumbnail", "original", "../original"} {
		requireStatus(t, request(variant, map[string]string{"If-None-Match": "*"}), http.StatusNotFound)
	}
	if _, err := h.blog.posts.SetPublished(t.Context(), post.ID, true); err != nil {
		t.Fatal(err)
	}
	var etags []string
	for _, part := range []struct {
		name string
		data []byte
	}{{"display", upload.Display}, {"thumbnail", upload.Thumbnail}} {
		w := request(part.name, nil)
		requireStatus(t, w, http.StatusOK)
		if !bytes.Equal(w.Body.Bytes(), part.data) {
			t.Fatal("wrong representation")
		}
		etag := w.Header().Get("ETag")
		etags = append(etags, etag)
		requireStatus(t, request(part.name, map[string]string{"If-None-Match": etag}), http.StatusNotModified)
		w = request(part.name, map[string]string{"Range": "bytes=0-3"})
		requireStatus(t, w, http.StatusPartialContent)
		if !bytes.Equal(w.Body.Bytes(), part.data[:4]) {
			t.Fatal("wrong range")
		}
	}
	if etags[0] == etags[1] {
		t.Fatal("different copies shared an ETag")
	}
	requireStatus(t, request("thumbnail", map[string]string{"If-None-Match": etags[0]}), http.StatusOK)
	requireStatus(t, request("original", nil), http.StatusNotFound)
	h.register(t)
	h.login(t)
	requireStatus(t, h.request("GET", "/images/"+id+"?variant=original", nil, false), http.StatusNotFound)
	h.verify(t)
	w := h.request("GET", "/images/"+id+"?variant=original", nil, false)
	requireStatus(t, w, http.StatusOK)
	if !bytes.Equal(w.Body.Bytes(), upload.Original) || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("original not privately downloadable")
	}
	if _, err := h.blog.posts.SetPublished(t.Context(), post.ID, false); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"display", "thumbnail", "original"} {
		requireStatus(t, request(variant, map[string]string{"If-None-Match": "*", "Range": "bytes=0-3"}), http.StatusNotFound)
	}
	if err := h.blog.posts.Delete(t.Context(), post.ID); err != nil {
		t.Fatal(err)
	}
	files, err := h.blog.media.List(t.Context())
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	w = h.request("POST", "/media/delete/"+id, url.Values{"version": {strconv.FormatInt(files[0].Version(), 10)}}, false)
	requireStatus(t, w, http.StatusSeeOther)
	for _, variant := range []imageVariant{displayImage, thumbnailImage, originalImage} {
		if _, err := h.blog.images.OpenVariant(t.Context(), id, variant); !errors.Is(err, errNotFound) {
			t.Fatalf("cleanup left %s: %v", variant, err)
		}
	}
}

// TestImageRegenerationCacheValidation prevents preserved upload dates from validating stale copies.
func TestImageRegenerationCacheValidation(t *testing.T) {
	h := newHarness(t)
	store := h.blog.images.(*fileImageStore)
	upload := imageUpload{MediaType: "image/png", Original: []byte("original"), Display: []byte("old display"), Thumbnail: []byte("old thumbnail")}
	uploaded := time.Unix(1000, 0)
	id, err := store.put(t.Context(), upload, uploaded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.blog.posts.Save(t.Context(), Post{Title: "Image", Slug: "image", PreviewImage: id}); err != nil {
		t.Fatal(err)
	}
	request := func(variant string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/images/"+id+"?variant="+variant, nil)
		for key, value := range headers {
			r.Header.Set(key, value)
		}
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		return w
	}
	oldETags := make(map[string]string)
	for _, variant := range []string{"display", "thumbnail"} {
		w := request(variant, nil)
		requireStatus(t, w, http.StatusOK)
		oldETags[variant] = w.Header().Get("ETag")
	}
	upload.Display, upload.Thumbnail = []byte("new display"), []byte("new thumbnail")
	if _, err := store.put(t.Context(), upload, uploaded); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"display", "thumbnail"} {
		t.Run(variant, func(t *testing.T) {
			for _, headers := range []map[string]string{
				{"If-Modified-Since": uploaded.UTC().Format(http.TimeFormat)},
				{"If-None-Match": oldETags[variant]},
				{"Range": "bytes=0-2", "If-Range": uploaded.UTC().Format(http.TimeFormat)},
			} {
				w := request(variant, headers)
				requireStatus(t, w, http.StatusOK)
				if w.Body.String() != "new "+variant || w.Header().Get("Last-Modified") != "" {
					t.Fatal("date-based validation returned stale or partial content", w.Body.String(), w.Header())
				}
				if etag := w.Header().Get("ETag"); etag == "" || etag == oldETags[variant] {
					t.Fatal("regeneration did not change the ETag")
				} else {
					requireStatus(t, request(variant, map[string]string{"If-None-Match": etag}), http.StatusNotModified)
				}
			}
		})
	}
}

// TestImageProcessingBusy checks the bound and confirms rejected requests do not release another upload's slot.
func TestImageProcessingBusy(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	h.blog.imageProcessing <- struct{}{}
	w := h.request("POST", "/post/images/upload", nil, false)
	requireStatus(t, w, http.StatusTooManyRequests)
	if w.Header().Get("Retry-After") != "1" || len(h.blog.imageProcessing) != 1 {
		t.Fatal("incorrect busy response")
	}
	<-h.blog.imageProcessing
	w = h.request("POST", "/post/images/upload", nil, false)
	requireStatus(t, w, http.StatusBadRequest)
	if len(h.blog.imageProcessing) != 0 {
		t.Fatal("failed upload retained its processing slot")
	}
}

// cancelDuringWrite triggers cancellation after Put's initial preflight check.
type cancelDuringWrite struct {
	context.Context
	checks int
}

func (c *cancelDuringWrite) Err() error {
	c.checks++
	if c.checks > 1 {
		return context.Canceled
	}
	return nil
}

// TestImageAtomicCopies keeps the last complete upload when a replacement fails.
func TestImageAtomicCopies(t *testing.T) {
	s := newTestImageStore(t)
	upload := imageUpload{MediaType: "image/png", Original: []byte("original"), Display: []byte("display"), Thumbnail: []byte("thumbnail")}
	id, err := s.Put(t.Context(), upload)
	if err != nil {
		t.Fatal(err)
	}
	upload.Display = []byte("replacement")
	if _, err := s.Put(&cancelDuringWrite{Context: t.Context()}, upload); !errors.Is(err, context.Canceled) {
		t.Fatalf("write cancellation: %v", err)
	}
	stored, err := s.Open(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(stored.Content)
	stored.Content.Close()
	if err != nil || string(body) != "display" {
		t.Fatal("failed replacement changed published data", err)
	}
	entries, err := os.ReadDir(s.root.Name())
	if err != nil || len(entries) != 1 {
		t.Fatal("staging files survived cancellation", entries, err)
	}
	for _, part := range []string{"original", "display", "thumbnail"} {
		incomplete := upload
		switch part {
		case "original":
			incomplete.Original = nil
		case "display":
			incomplete.Display = nil
		case "thumbnail":
			incomplete.Thumbnail = nil
		}
		if _, err := s.Put(t.Context(), incomplete); err == nil {
			t.Fatal("incomplete upload accepted")
		}
	}
}

// TestOptimizeImages checks safe conversion, repeatability, and filesystem backup/restore.
func TestOptimizeImages(t *testing.T) {
	directory := t.TempDir()
	data := testPNG(t)
	hash := sha256.Sum256(data)
	id := hex.EncodeToString(hash[:]) + ".png"
	path := filepath.Join(directory, id)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	uploaded := time.Unix(1000, 0)
	if err := os.Chtimes(path, uploaded, uploaded); err != nil {
		t.Fatal(err)
	}
	s, err := openImageStore(directory, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Open(t.Context(), id); !errors.Is(err, errImageNotOptimized) {
		t.Fatalf("legacy original served: %v", err)
	}
	for range 2 {
		count, err := OptimizeImages(t.Context(), directory, "")
		if err != nil || count != 1 {
			t.Fatalf("regeneration: %d %v", count, err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(uploaded) {
		t.Fatal("regeneration changed the upload date", err)
	}
	backup := t.TempDir()
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, id), stored, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := openImageStore(backup, "")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	prepared, err := prepareImage(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []struct {
		variant imageVariant
		data    []byte
	}{{originalImage, data}, {displayImage, prepared.Display}, {thumbnailImage, prepared.Thumbnail}} {
		file, err := restored.OpenVariant(t.Context(), id, part.variant)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(file.Content)
		file.Content.Close()
		if err != nil || !bytes.Equal(body, part.data) {
			t.Fatalf("restored %s: %v", part.variant, err)
		}
	}
	if err := os.WriteFile(path, []byte("corrupt original"), 0600); err != nil {
		t.Fatal(err)
	}
	count, err := OptimizeImages(t.Context(), directory, "")
	if count != 0 || err == nil {
		t.Fatal("changed original regenerated", count, err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != "corrupt original" {
		t.Fatal("failed conversion replaced original")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := OptimizeImages(ctx, directory, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("regeneration cancellation: %v", err)
	}
}
