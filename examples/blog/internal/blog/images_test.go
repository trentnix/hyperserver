package blog

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

// TestImageHTTPResponses exercises the public route, including standard file response semantics.
func TestImageHTTPResponses(t *testing.T) {
	h := newHarness(t)
	data := testPNG(t)
	id, err := h.blog.images.Put(t.Context(), "image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.blog.posts.Save(t.Context(), Post{Slug: "image", Title: "Image", PreviewImage: id}); err != nil {
		t.Fatal(err)
	}
	request := func(method string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/images/"+id, nil)
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		return w
	}
	first := request(http.MethodGet, nil)
	requireStatus(t, first, http.StatusOK)
	if !bytes.Equal(first.Body.Bytes(), data) || first.Header().Get("Content-Type") != "image/png" || first.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("incorrect public image response")
	}
	etag, modified := first.Header().Get("ETag"), first.Header().Get("Last-Modified")
	if etag == "" || modified == "" || first.Header().Get("Cache-Control") != "public, no-cache" {
		t.Fatalf("missing cache headers: %v", first.Header())
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, headers := range []map[string]string{
			{"If-None-Match": etag},
			{"If-None-Match": "W/" + etag},
			{"If-None-Match": `"old", ` + etag},
			{"If-None-Match": "*"},
			{"If-Modified-Since": modified},
		} {
			w := request(method, headers)
			requireStatus(t, w, http.StatusNotModified)
			if w.Body.Len() != 0 || w.Header().Get("ETag") != etag || w.Header().Get("Cache-Control") != first.Header().Get("Cache-Control") {
				t.Fatalf("incorrect conditional response: %v", w.Header())
			}
		}
	}
	head := request(http.MethodHead, nil)
	requireStatus(t, head, http.StatusOK)
	if head.Body.Len() != 0 || head.Header().Get("Content-Length") != first.Header().Get("Content-Length") {
		t.Fatal("HEAD must describe the image without sending bytes")
	}
	w := request(http.MethodGet, map[string]string{"If-None-Match": `"different"`, "If-Modified-Since": modified})
	requireStatus(t, w, http.StatusOK)
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatal("mismatched ETag did not return the image")
	}
	w = request(http.MethodGet, map[string]string{"Range": "bytes=0-7", "If-Range": etag})
	requireStatus(t, w, http.StatusPartialContent)
	if !bytes.Equal(w.Body.Bytes(), data[:8]) || w.Header().Get("Content-Range") == "" {
		t.Fatal("incorrect byte range")
	}
	w = request(http.MethodGet, map[string]string{"Range": "bytes=0-7", "If-Range": `"different"`})
	requireStatus(t, w, http.StatusOK)
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatal("mismatched If-Range did not return the complete image")
	}
	w = request(http.MethodGet, map[string]string{"Range": "bytes=999999999-"})
	requireStatus(t, w, http.StatusRequestedRangeNotSatisfiable)
	if strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatal("range error advertised immutable content")
	}
	id = strings.Repeat("a", 64) + ".png"
	w = request(http.MethodGet, nil)
	requireStatus(t, w, http.StatusNotFound)
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
		t.Fatal("missing image advertised cached content")
	}
}

type failingImageStore struct{}

func (failingImageStore) Put(context.Context, string, []byte) (string, error) {
	return "", errors.New("private image storage failure")
}

func (failingImageStore) Open(context.Context, string) (storedImage, error) {
	return storedImage{}, errors.New("private image storage failure")
}

// TestImageStorageHTTPFailures checks errors from the replaceable image storage boundary.
func TestImageStorageHTTPFailures(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	h.blog.images = failingImageStore{}
	w := h.request(http.MethodGet, "/images/"+strings.Repeat("a", 64)+".png", nil, false)
	requireStatus(t, w, http.StatusInternalServerError)
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "private image") {
		t.Fatal("failed image read was cacheable or exposed storage details")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "picture.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(testPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/post/images/upload", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	for _, cookie := range h.cookies {
		r.AddCookie(cookie)
	}
	w = httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	requireStatus(t, w, http.StatusInternalServerError)
	if strings.Contains(w.Body.String(), "private image") || strings.Contains(w.Body.String(), "filePath") {
		t.Fatal("failed upload exposed storage details or a success URL")
	}
}
