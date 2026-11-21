package blog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestImageUploadAuthentication(t *testing.T) {
	h := newHarness(t)
	assertFailure := func(status int, code string) {
		t.Helper()
		for _, hx := range []bool{false, true} {
			w := h.request("POST", "/post/images/upload", nil, hx)
			requireStatus(t, w, status)
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["code"] != code || body["error"] == "" {
				t.Fatalf("missing upload authentication response: %s, %v", w.Body, err)
			}
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
				t.Fatalf("upload denial must not redirect or be cached: %v", w.Header())
			}
		}
	}
	assertFailure(http.StatusUnauthorized, "authentication_required")
	h.register(t)
	h.login(t)
	assertFailure(http.StatusForbidden, "verification_required")
	h.verify(t)
	// A valid login reaches multipart validation, not the access denial.
	requireStatus(t, h.request("POST", "/post/images/upload", nil, false), http.StatusBadRequest)
	oldCookie := *h.cookies["auth-user-session"]
	requireStatus(t, h.request("POST", "/auth/logout", nil, false), http.StatusSeeOther)
	// Keep presenting a well-formed cookie whose server-side session is gone.
	h.cookies[oldCookie.Name] = &oldCookie
	assertFailure(http.StatusUnauthorized, "authentication_required")
	h.login(t)
	requireStatus(t, h.request("POST", "/post/images/upload", nil, false), http.StatusBadRequest)

	// Being authenticated does not grant upload access to a different account.
	r := user.AddUserToRequestContext(httptest.NewRequest("POST", "/post/images/upload", nil), &user.User{Email: "other@example.invalid", Verified: true})
	w := httptest.NewRecorder()
	h.blog.upload(w, r)
	requireStatus(t, w, http.StatusForbidden)
	if !strings.Contains(w.Body.String(), `"code":"access_denied"`) {
		t.Fatal("non-administrator upload did not explain the access denial", w.Body)
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

// TestMediaUploadForms keeps ordinary and HTMX uploads separate from EasyMDE's JSON contract.
func TestMediaUploadForms(t *testing.T) {
	h := newHarness(t)
	request := func(data []byte, hx bool) *httptest.ResponseRecorder {
		return requestImageUpload(t, h, "/media/upload", data, hx)
	}
	requireStatus(t, request(testPNG(t), false), http.StatusForbidden)
	h.register(t)
	h.login(t)
	requireStatus(t, request(testPNG(t), true), http.StatusForbidden)
	h.verify(t)
	for _, hx := range []bool{false, true} {
		failure := request([]byte("not an image"), hx)
		requireStatus(t, failure, http.StatusUnprocessableEntity)
		if !strings.Contains(failure.Body.String(), "Choose a valid PNG") || !strings.HasPrefix(failure.Header().Get("Content-Type"), "text/html") || strings.Contains(failure.Body.String(), "<!DOCTYPE") == hx {
			t.Fatal("upload error must use the appropriate HTML response", failure.Body.String())
		}
		response := request(testPNG(t), hx)
		status, header := http.StatusSeeOther, "Location"
		if hx {
			status, header = http.StatusOK, "HX-Redirect"
		}
		requireStatus(t, response, status)
		path := response.Header().Get(header)
		if !strings.HasPrefix(path, "/media/") || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("upload must navigate to private image details", response.Header())
		}
		detail := h.request("GET", path, nil, false)
		requireStatus(t, detail, http.StatusOK)
		if !strings.Contains(detail.Body.String(), "Image uploaded. Labels and tags are optional.") {
			t.Fatal("image details must acknowledge the completed upload")
		}
	}
	h.blog.images = failingImageStore{}
	failed := request(testPNG(t), true)
	requireStatus(t, failed, http.StatusInternalServerError)
	if strings.Contains(failed.Body.String(), "private image") || failed.Header().Get("HX-Redirect") != "" {
		t.Fatal("failed upload exposed storage details or reported success")
	}
}

func requestImageUpload(t *testing.T, h *harness, path string, data []byte, hx bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "picture.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, path, &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	if hx {
		r.Header.Set("HX-Request", "true")
		r.Header.Set("HX-Target", "div#library-upload-status")
	}
	for _, cookie := range h.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	for _, cookie := range w.Result().Cookies() {
		h.cookies[cookie.Name] = cookie
	}
	return w
}

func TestSharedUploadQuota(t *testing.T) {
	h := usersHarness(t)
	h.blog.settings.FileStorageLimitMiB = 1
	data := testPNG(t)
	store := h.blog.downloads.(*fileDownloadStore)
	file, err := store.root.OpenFile(strings.Repeat("f", 64)+".pdf", os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	limit := h.blog.fileStorageLimit()
	// The original fits, but its bundle also contains the generated copies.
	err = file.Truncate(limit - int64(len(data)))
	if closeErr := file.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	for _, path := range []string{"/media/upload", "/post/images/upload"} {
		for _, hx := range []bool{false, true} {
			w := requestImageUpload(t, h, path, data, hx)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			if w.Header().Get("Upload-Storage-Full") != "" {
				t.Fatal("a large image must not report full storage if a smaller upload may fit")
			}
			if !strings.Contains(w.Body.String(), "Upload storage has") {
				t.Fatal("missing quota feedback", w.Body)
			}
		}
	}
	id := imageUploadID(testImageUpload("image/png", data))
	metadata, err := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), id)
	if err != nil || metadata.Version != 0 {
		t.Fatal("rejected upload reserved metadata", metadata, err)
	}
	if _, err := h.blog.images.Open(t.Context(), id); !errors.Is(err, errNotFound) {
		t.Fatal("rejected upload published an image", err)
	}
	if err := store.root.Remove(strings.Repeat("f", 64) + ".pdf"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, requestImageUpload(t, h, "/post/images/upload", data, false), http.StatusOK)
	files, err := h.blog.media.List(t.Context())
	if err != nil || len(files) != 1 || files[0].Size <= int64(len(data)) {
		t.Fatal("expected a stored image bundle", files, err)
	}
	remaining := int(limit - files[0].Size)
	requireStatus(t, uploadTestDownload(t, h, "oversized.txt", bytes.Repeat([]byte("x"), remaining+1)), http.StatusUnprocessableEntity)
	requireStatus(t, uploadTestDownload(t, h, "exact-fit.txt", bytes.Repeat([]byte("x"), remaining)), http.StatusOK)
	requireStatus(t, requestImageUpload(t, h, "/post/images/upload", data, false), http.StatusOK)
}

func TestConcurrentUploadsShareQuota(t *testing.T) {
	h := usersHarness(t)
	h.blog.settings.FileStorageLimitMiB = 1
	image := testPNG(t)
	file := bytes.Repeat([]byte("x"), int(h.blog.fileStorageLimit()))
	var imageResponse, fileResponse *httptest.ResponseRecorder
	start := make(chan struct{})
	var uploads sync.WaitGroup
	uploads.Go(func() {
		<-start
		imageResponse = requestImageUpload(t, h, "/post/images/upload", image, false)
	})
	uploads.Go(func() {
		<-start
		fileResponse = uploadTestDownload(t, h, "full-size.txt", file)
	})
	close(start)
	uploads.Wait()
	statuses := [2]int{imageResponse.Code, fileResponse.Code}
	if statuses != [2]int{http.StatusOK, http.StatusUnprocessableEntity} && statuses != [2]int{http.StatusUnprocessableEntity, http.StatusOK} {
		t.Fatalf("exactly one upload must fit, got statuses %v", statuses)
	}
	files, err := h.blog.media.List(t.Context())
	if err != nil || len(files) != 1 || fileStorageUsed(files) > h.blog.fileStorageLimit() {
		t.Fatal("concurrent uploads must stay within the shared limit", files, err)
	}
}

// TestImageHTTPResponses exercises the public route, including standard file response semantics.
func TestImageHTTPResponses(t *testing.T) {
	h := newHarness(t)
	data := testPNG(t)
	id, err := h.blog.images.Put(t.Context(), testImageUpload("image/png", data), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := savePostFixture(h.blog.posts, t.Context(), Post{Slug: "image", Title: "Image", PreviewImage: id}); err != nil {
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
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Last-Modified") != "" || first.Header().Get("Cache-Control") != "public, no-cache" {
		t.Fatalf("incorrect cache headers: %v", first.Header())
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, headers := range []map[string]string{
			{"If-None-Match": etag},
			{"If-None-Match": "W/" + etag},
			{"If-None-Match": `"old", ` + etag},
			{"If-None-Match": "*"},
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
	w := request(http.MethodGet, map[string]string{"If-None-Match": `"different"`, "If-Modified-Since": "Wed, 21 Oct 2099 07:28:00 GMT"})
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

func (failingImageStore) Put(context.Context, imageUpload, func(int64) error) (string, error) {
	return "", errors.New("private image storage failure")
}

func (failingImageStore) Open(context.Context, string) (storedMedia, error) {
	return storedMedia{}, errors.New("private image storage failure")
}

func (s failingImageStore) OpenVariant(ctx context.Context, id string, _ imageVariant) (storedMedia, error) {
	return s.Open(ctx, id)
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

// TestImageUploadMetadataFailure reports naming failure without deleting an existing upload.
func TestImageUploadMetadataFailure(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	data := testPNG(t)
	id, err := h.blog.images.Put(t.Context(), testImageUpload("image/png", data), nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata := h.blog.mediaMetadata
	h.blog.mediaMetadata = failedMediaMetadata{}
	upload := func() *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("image", "Retry.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/post/images/upload", &body)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		for _, cookie := range h.cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		return w
	}
	w := upload()
	requireStatus(t, w, http.StatusInternalServerError)
	if strings.Contains(w.Body.String(), "private metadata") || strings.Contains(w.Body.String(), "filePath") || !strings.Contains(w.Body.String(), "Retry") {
		t.Fatal("incorrect upload failure")
	}
	stored, err := h.blog.images.Open(t.Context(), id)
	if err != nil {
		t.Fatal("metadata failure removed the existing upload", err)
	}
	stored.Content.Close()
	h.blog.mediaMetadata = metadata
	requireStatus(t, upload(), http.StatusOK)
	name, err := metadata.GetMediaMetadata(t.Context(), id)
	if err != nil || name.Filename != "Retry.png" {
		t.Fatal("retry did not recover", name, err)
	}
}
