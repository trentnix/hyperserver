package blog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

type rejectedHistoryLookup struct{ imageAccessRepository }

type countedImageWrites struct {
	imageStore
	writes int
	fail   bool
}

func (s *countedImageWrites) Put(ctx context.Context, upload imageUpload) (string, error) {
	s.writes++
	if s.fail {
		return "", errors.New("image write failed")
	}
	return s.imageStore.Put(ctx, upload)
}

func (rejectedHistoryLookup) ImageInAuthorsPosts(context.Context, string, ...string) (bool, error) {
	return false, errors.New("public images must not query private history")
}

func TestPosterHistoricalImage(t *testing.T) {
	h := usersHarness(t)
	admin, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	imageID, err := h.blog.images.Put(t.Context(), testImageUpload("image/png", testPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err = h.blog.mediaMetadata.RememberMedia(t.Context(), imageID, "admin.png", admin.ID); err != nil {
		t.Fatal(err)
	}
	poster := registeredPoster(t, h)
	p, err := h.blog.writePost(t.Context(), Post{Title: "Poster draft", Slug: "poster-review", Markdown: "![Image](/images/" + imageID + ")", Draft: true, AuthorID: poster.ID})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("GET", "/images/"+imageID, nil, false), http.StatusOK)
	p.Markdown = "Image removed"
	p, err = h.blog.writePost(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	posterCookies := h.cookies
	h.cookies = make(map[string]*http.Cookie)
	requireStatus(t, h.request("GET", "/images/"+imageID, nil, false), http.StatusNotFound)
	h.cookies = posterCookies
	requireStatus(t, h.request("GET", "/images/"+imageID, nil, false), http.StatusOK)
	requireStatus(t, h.request("GET", "/images/"+imageID+"?variant=thumbnail", nil, false), http.StatusOK)
	requireStatus(t, h.request("GET", "/images/"+imageID+"?variant=original", nil, false), http.StatusNotFound)
	revisions, err := h.blog.history.ListRevisions(t.Context(), "post", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := revisions[len(revisions)-1]
	path := "/history/post/" + p.ID + "/restore?revision=" + strconv.FormatInt(old.ID, 10)
	w := h.request("POST", path, url.Values{"version": {strconv.FormatInt(p.Version, 10)}}, false)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("owner cannot restore their saved image: HTTP %d", w.Code)
	}
	saved, err := h.blog.posts.GetByID(t.Context(), p.ID, true)
	if err != nil || !strings.Contains(saved.Markdown, imageID) {
		t.Fatalf("restored image missing: %+v %v", saved, err)
	}
}

func TestPublicReuseDoesNotGrantOriginal(t *testing.T) {
	h := usersHarness(t)
	admin, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	id, err := h.blog.images.Put(t.Context(), testImageUpload("image/png", testPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err = h.blog.mediaMetadata.RememberMedia(t.Context(), id, "admin.png", admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = h.blog.writePost(t.Context(), Post{Title: "Public admin image", Slug: "public-admin-review", Markdown: "Body", AuthorID: admin.ID, PreviewImage: id}); err != nil {
		t.Fatal(err)
	}
	registeredPoster(t, h)
	h.blog.imageAccess = rejectedHistoryLookup{h.blog.imageAccess}
	for _, suffix := range []string{"", "?variant=thumbnail"} {
		requireStatus(t, h.request("GET", "/images/"+id+suffix, nil, false), http.StatusOK)
	}
	requireStatus(t, h.request("GET", "/images/"+id+"?variant=original", nil, false), http.StatusNotFound)
	fields := url.Values{"title": {"Reuse public image"}, "content": {"My draft"}, "previewImage": {id}}
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusSeeOther)
	w := h.request("GET", "/images/"+id+"?variant=original", nil, false)
	if w.Code != http.StatusNotFound {
		t.Fatalf("adding public image to own draft grants another uploader's original: HTTP %d", w.Code)
	}
}

func TestBrowserPosterUpload(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check Poster image controls")
	}
	for _, width := range []string{"320", "1100"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "poster-upload") })
	}
}

func TestPosterImageUploads(t *testing.T) {
	h := usersHarness(t)
	images := &countedImageWrites{imageStore: h.blog.images}
	h.blog.images = images
	adminCookies := h.cookies
	firstLink := inviteUserForTest(t, h, "first@example.test", "poster")
	secondLink := inviteUserForTest(t, h, "second@example.test", "poster")
	login := func(link, email string) {
		t.Helper()
		h.cookies = make(map[string]*http.Cookie)
		requireStatus(t, h.request("GET", link, nil, false), http.StatusSeeOther)
		requireStatus(t, h.request("POST", "/register", credentials(email), false), http.StatusSeeOther)
		requireStatus(t, h.request("POST", "/login", credentials(email), false), http.StatusSeeOther)
	}
	image := testPNG(t)
	upload := func(status int) string {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("image", "photo.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(image); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteField("uploaded_by", "forged-account"); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "https://blog.example/post/images/upload", &body)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		for _, cookie := range h.cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		requireStatus(t, w, status)
		if status != http.StatusOK {
			if strings.Contains(w.Body.String(), "filePath") {
				t.Fatal("failed upload returned an image URL")
			}
			return ""
		}
		var result struct{ Data struct{ FilePath string } }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return strings.TrimPrefix(result.Data.FilePath, "/images/")
	}

	login(firstLink, "first@example.test")
	firstCookies := h.cookies
	first, _ := h.app.Accounts.GetByEmail(t.Context(), "first@example.test")
	editor := h.request("GET", "/post/create", nil, false)
	requireStatus(t, editor, http.StatusOK)
	if !strings.Contains(editor.Body.String(), "data-image-upload") || strings.Contains(editor.Body.String(), "data-media-browse") {
		t.Fatal("poster needs upload controls, not the shared library")
	}
	metadataStore := h.blog.mediaMetadata
	h.blog.mediaMetadata = failedMediaMetadata{}
	upload(http.StatusInternalServerError)
	if images.writes != 0 {
		t.Fatal("metadata lookup failure wrote image bytes")
	}
	h.blog.mediaMetadata = metadataStore
	images.fail = true
	upload(http.StatusInternalServerError)
	images.fail = false
	id := upload(http.StatusOK)
	metadata, err := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), id)
	if err != nil || metadata.UploadedBy != first.ID {
		t.Fatalf("upload attribution: %+v %v", metadata, err)
	}
	for _, suffix := range []string{"", "?variant=thumbnail", "?variant=original"} {
		requireStatus(t, h.request("GET", "/images/"+id+suffix, nil, false), http.StatusOK)
	}
	if duplicate := upload(http.StatusOK); duplicate != id {
		t.Fatal("own duplicate changed image ID")
	}
	fields := url.Values{"title": {"My image"}, "content": {"![Photo](/images/" + id + ")"}, "previewImage": {id}}
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusSeeOther)

	h.cookies = make(map[string]*http.Cookie)
	requireStatus(t, h.request("GET", "/images/"+id, nil, false), http.StatusNotFound)
	login(secondLink, "second@example.test")
	secondCookies := h.cookies
	for _, suffix := range []string{"", "?variant=thumbnail", "?variant=original"} {
		requireStatus(t, h.request("GET", "/images/"+id+suffix, nil, false), http.StatusNotFound)
	}
	beforeWrites := images.writes
	beforeMetadata, err := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	upload(http.StatusConflict)
	afterMetadata, err := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), id)
	if err != nil || afterMetadata != beforeMetadata || images.writes != beforeWrites {
		t.Fatal("denied duplicate changed the image or metadata", err)
	}
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusUnprocessableEntity)
	for _, path := range []string{"/media", "/media/" + id, "/media/delete/" + id} {
		requireStatus(t, h.request("GET", path, nil, false), http.StatusForbidden)
	}

	h.cookies = adminCookies
	metadata.UploadedBy = "forged-account"
	metadata.Label = "Admin label"
	if err := h.blog.mediaMetadata.SaveMediaMetadata(t.Context(), id, metadata); err != nil {
		t.Fatal(err)
	}
	if duplicate := upload(http.StatusOK); duplicate != id {
		t.Fatal("admin duplicate changed image ID")
	}
	metadata, _ = h.blog.mediaMetadata.GetMediaMetadata(t.Context(), id)
	if metadata.UploadedBy != first.ID {
		t.Fatal("metadata edit or duplicate transferred ownership")
	}
	requireStatus(t, h.request("POST", "/users/"+first.ID+"/remove", url.Values{"version": {strconv.FormatInt(first.SessionVersion, 10)}}, false), http.StatusSeeOther)
	h.cookies = firstCookies
	requireStatus(t, h.request("GET", "/images/"+id, nil, false), http.StatusNotFound)
	upload(http.StatusUnauthorized)

	// A public use makes duplicate bytes reusable without changing their uploader.
	if _, err := h.blog.writePost(t.Context(), Post{Title: "Public image", Slug: "public-image", Markdown: "Photo", PreviewImage: id}); err != nil {
		t.Fatal(err)
	}
	h.cookies = secondCookies
	h.blog.imageAccess = rejectedHistoryLookup{h.blog.imageAccess}
	upload(http.StatusOK)
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusSeeOther)
	metadata, _ = h.blog.mediaMetadata.GetMediaMetadata(t.Context(), id)
	if metadata.UploadedBy != first.ID {
		t.Fatal("public reuse transferred ownership")
	}
}
