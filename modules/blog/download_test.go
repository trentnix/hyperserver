package blog

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func uploadTestDownload(t *testing.T, h *harness, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "https://blog.example/post/files/upload", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	for _, cookie := range h.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	return w
}

func testDownload(t *testing.T, h *harness, name, data string) postDownload {
	t.Helper()
	w := uploadTestDownload(t, h, name, []byte(data))
	requireStatus(t, w, http.StatusOK)
	var entry postDownload
	if err := json.Unmarshal(w.Body.Bytes(), &entry); err != nil || !validDownloadID(entry.ID) {
		t.Fatal("invalid upload", w.Body.String(), err)
	}
	return entry
}

func downloadSource(t *testing.T, entries ...postDownload) string {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDownloadValidation(t *testing.T) {
	var zipped bytes.Buffer
	archive := zip.NewWriter(&zipped)
	part, _ := archive.Create("../not-extracted.txt")
	part.Write([]byte("archive contents"))
	archive.Close()
	for _, tc := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"notes.txt", []byte("Plain text\nwith Unicode: café"), true},
		{"empty.txt", nil, true},
		{"guide.PDF", []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n"), true},
		{"archive.zip", zipped.Bytes(), true},
		{"script.txt", []byte("<html><script>alert(1)</script>"), false},
		{"binary.txt", []byte{0, 1, 2}, false},
		{"invalid.txt", []byte{255}, false},
		{"notes.pdf", []byte("renaming text does not make a PDF"), false},
		{"broken.pdf", []byte("%PDF-1.7\n"), false},
		{"broken.zip", []byte("PK\x03\x04"), false},
		{"notes.html", []byte("plain text"), false},
		{"large.txt", bytes.Repeat([]byte("x"), maxDownloadBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extension, err := validateDownload(tc.data, tc.name)
			if (err == nil) != tc.valid {
				t.Fatalf("extension %q, error %v", extension, err)
			}
		})
	}
}

func TestDownloadStore(t *testing.T) {
	images, err := openImageStore(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer images.Close()
	store, err := openDownloadStore(images.root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.root.Close()
	data := []byte("immutable file")
	id, err := store.Put(t.Context(), data, ".txt")
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.Open(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := file.Stat()
	got, _ := io.ReadAll(file)
	file.Close()
	if !bytes.Equal(got, data) {
		t.Fatal("file changed")
	}
	if _, err := store.Put(t.Context(), data, ".txt"); err != nil {
		t.Fatal(err)
	}
	files, err := store.List(t.Context())
	if err != nil || len(files) != 1 || !files[0].Modified.Equal(info.ModTime()) {
		t.Fatal("duplicate upload changed identity or date", files, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Put(ctx, []byte("canceled"), ".txt"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, id := range []string{"../file.txt", strings.Repeat("A", 64) + ".pdf", strings.Repeat("a", 64) + ".txt/other.pdf", strings.Repeat("a", 64) + ".extra.pdf"} {
		if _, err := store.Open(t.Context(), id); !errors.Is(err, errNotFound) {
			t.Fatal("unsafe ID", id, err)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	link := strings.Repeat("b", 64) + ".txt"
	if err := store.root.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(t.Context(), link); !errors.Is(err, errNotFound) {
		t.Fatal("symlink opened", err)
	}
	files, err = store.List(t.Context())
	if err != nil || len(files) != 1 {
		t.Fatal("symlink listed", files, err)
	}
}

func TestDownloadLifecycle(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			entry := testDownload(t, h, "Guide résumé.txt", "A downloadable guide with Unicode.")
			path := "/files/" + entry.ID
			public := func(method string, headers map[string]string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, "https://blog.example"+path, nil)
				for k, v := range headers {
					r.Header.Set(k, v)
				}
				w := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(w, r)
				return w
			}
			private := func() {
				for _, method := range []string{"GET", "HEAD"} {
					for _, headers := range []map[string]string{nil, {"Range": "bytes=0-2"}, {"If-None-Match": "*"}, {"If-Modified-Since": "Wed, 21 Oct 2099 07:28:00 GMT"}} {
						w := public(method, headers)
						requireStatus(t, w, http.StatusNotFound)
						if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
							t.Fatal("private cache validators", w.Header())
						}
					}
				}
			}
			private()
			admin := h.request("GET", path, nil, false)
			requireStatus(t, admin, http.StatusOK)
			kind, params, err := mime.ParseMediaType(admin.Header().Get("Content-Disposition"))
			if err != nil || kind != "attachment" || params["filename"] != entry.Label || admin.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("unsafe attachment", admin.Header(), err)
			}
			photo := mediaUpload(t, h.blog, "album with downloads")
			album := "![Keep photo](/images/" + photo.ID + ")"
			fields := url.Values{"title": {"Files and photos"}, "slug": {"files-and-photos"}, "content": {""}, "album": {album}, "downloads": {downloadSource(t, entry)}}
			w := h.request("POST", "/post", fields, hx)
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, w, status)
			post, err := h.blog.posts.GetBySlug(t.Context(), "files-and-photos", true)
			if err != nil {
				t.Fatal(err)
			}
			private()
			usage, err := h.blog.mediaReferences.ReferencedMedia(t.Context(), entry.ID, photo.ID)
			if err != nil || !usage[entry.ID].Current || !usage[photo.ID].Current {
				t.Fatal("references missing", usage, err)
			}
			editor := h.request("GET", "/post/update/"+post.ID, nil, false).Body.String()
			if strings.Contains(editor, `required>\n\n`+"```post-components") || !strings.Contains(editor, "Keep photo") || !strings.Contains(editor, "Guide résumé.txt") {
				t.Fatal("components not editable separately")
			}
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			get := public("GET", nil)
			requireStatus(t, get, http.StatusOK)
			if get.Header().Get("Cache-Control") != "public, no-cache" || get.Body.String() != "A downloadable guide with Unicode." {
				t.Fatal("public file missing")
			}
			requireStatus(t, public("GET", map[string]string{"If-None-Match": get.Header().Get("ETag")}), http.StatusNotModified)
			rangeResult := public("GET", map[string]string{"Range": "bytes=2-8"})
			requireStatus(t, rangeResult, http.StatusPartialContent)
			if rangeResult.Body.String() != "downloa" {
				t.Fatal("wrong range", rangeResult.Body.String())
			}
			head := public("HEAD", nil)
			requireStatus(t, head, http.StatusOK)
			if head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
				t.Fatal("HEAD failed")
			}
			for _, partial := range []bool{false, true} {
				page := h.request("GET", "/post/s/files-and-photos", nil, partial)
				requireStatus(t, page, http.StatusOK)
				if !strings.Contains(page.Body.String(), `class="file-downloads"`) || !strings.Contains(page.Body.String(), `data-album-photo`) {
					t.Fatal("components not rendered")
				}
			}
			if err := h.blog.removeMedia(t.Context(), entry.ID, time.Now().UnixNano(), true); !errors.Is(err, errMediaUsed) {
				t.Fatal(err)
			}
			post.Markdown = "No attachments now"
			if _, err = h.blog.persistPost(t.Context(), post); err != nil {
				t.Fatal(err)
			}
			private()
			usage, err = h.blog.mediaReferences.ReferencedMedia(t.Context(), entry.ID)
			if err != nil || usage[entry.ID].Current || len(usage[entry.ID].History) == 0 {
				t.Fatal("history reference missing", usage, err)
			}
			file, err := h.blog.downloads.Open(t.Context(), entry.ID)
			if err != nil {
				t.Fatal(err)
			}
			info, _ := file.Stat()
			file.Close()
			if err = h.blog.removeMedia(t.Context(), entry.ID, info.ModTime().UnixNano(), false); !errors.Is(err, errMediaHistory) {
				t.Fatal(err)
			}
			requireStatus(t, h.request("GET", "/media/delete/"+entry.ID, nil, false), http.StatusOK)
			if err = h.blog.removeMedia(t.Context(), entry.ID, info.ModTime().UnixNano(), true); err != nil {
				t.Fatal(err)
			}
			if _, err = h.blog.persistPost(t.Context(), Post{Title: "Missing file", Markdown: "[download](" + path + ")", Draft: true}); !errors.Is(err, errMissingMedia) {
				t.Fatal("missing download allowed", err)
			}
			versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
			if err != nil {
				t.Fatal(err)
			}
			history := h.request("GET", "/history/post/"+post.ID+"?revision="+strconv.FormatInt(versions[len(versions)-1].ID, 10), nil, false)
			requireStatus(t, history, http.StatusOK)
			if !strings.Contains(history.Body.String(), `aria-disabled="true"`) || !strings.Contains(history.Body.String(), "File deleted") {
				t.Fatal("history does not identify a deleted attachment")
			}
		})
	}
}

func TestDownloadStorageLimit(t *testing.T) {
	h := usersHarness(t)
	store := h.blog.downloads.(*fileDownloadStore)
	id := strings.Repeat("c", 64) + ".txt"
	file, err := store.root.OpenFile(id, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxDownloadStorage); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	w := uploadTestDownload(t, h, "over-quota.txt", []byte("one more file"))
	requireStatus(t, w, http.StatusUnprocessableEntity)
	if !strings.Contains(w.Body.String(), "1 GiB") {
		t.Fatal("storage limit not explained")
	}
	metadata, err := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), downloadID([]byte("one more file"), ".txt"))
	if err != nil || metadata.Version != 0 {
		t.Fatal("rejected upload reserved metadata", metadata, err)
	}
	if err := store.root.Remove(id); err != nil {
		t.Fatal(err)
	}
	// Existing bytes without ownership metadata cannot be claimed by a Poster.
	data := []byte("unowned file bytes")
	if _, err := store.Put(t.Context(), data, ".txt"); err != nil {
		t.Fatal(err)
	}
	registeredPoster(t, h)
	requireStatus(t, uploadTestDownload(t, h, "unowned.txt", data), http.StatusConflict)
}

func TestDownloadAccessAndReuse(t *testing.T) {
	h := usersHarness(t)
	entry := testDownload(t, h, "private.txt", "Private administrator file")
	poster := registeredPoster(t, h)
	requireStatus(t, h.request("GET", "/files/"+entry.ID, nil, false), http.StatusNotFound)
	requireStatus(t, uploadTestDownload(t, h, "duplicate.txt", []byte("Private administrator file")), http.StatusConflict)
	listing := h.request("GET", "/post/files", nil, false)
	requireStatus(t, listing, http.StatusOK)
	if strings.Contains(listing.Body.String(), entry.ID) {
		t.Fatal("private library entry exposed")
	}
	ctx := context.WithValue(t.Context(), user.UserContextKey, poster)
	source, err := joinPostComponents("", "", downloadSource(t, entry), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.blog.writePost(ctx, Post{Title: "Steal private file", Markdown: source, Draft: true}); err == nil {
		t.Fatal("foreign private file attached")
	}
	p, err := h.blog.persistPost(t.Context(), Post{Title: "Admin file in poster post", AuthorID: poster.ID, Markdown: source, Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("GET", "/files/"+entry.ID, nil, false), http.StatusOK)
	requireStatus(t, uploadTestDownload(t, h, "duplicate.txt", []byte("Private administrator file")), http.StatusOK)
	meta, _ := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), entry.ID)
	if meta.UploadedBy == poster.ID || meta.Filename != "private.txt" {
		t.Fatal("duplicate stole ownership", meta)
	}
	p.Markdown = "Removed from this post"
	if _, err = h.blog.persistPost(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("GET", "/files/"+entry.ID, nil, false), http.StatusOK)
	listing = h.request("GET", "/post/files", nil, false)
	if !strings.Contains(listing.Body.String(), entry.ID) {
		t.Fatal("history reuse missing")
	}
	other := testDownload(t, h, "poster.txt", "Poster's own file")
	requireStatus(t, h.request("GET", "/files/"+other.ID, nil, false), http.StatusOK)
	metadata, _ := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), other.ID)
	if metadata.UploadedBy != poster.ID {
		t.Fatal("uploader not recorded")
	}
}

func TestDownloadFormValidation(t *testing.T) {
	h := newHarness(t)
	requireStatus(t, uploadTestDownload(t, h, "notes.txt", []byte("private")), http.StatusUnauthorized)
	h.register(t)
	h.verify(t)
	h.login(t)
	entry := testDownload(t, h, "notes.txt", "Real file size")
	for _, hx := range []bool{false, true} {
		for _, source := range []string{"not JSON", downloadSource(t, postDownload{ID: entry.ID, Label: "Keep name", Size: entry.Size + 1}), downloadSource(t, postDownload{ID: strings.Repeat("b", 64) + ".txt", Label: "Missing", Size: 1})} {
			w := h.request("POST", "/post", url.Values{"title": {"Preserve title"}, "content": {"Preserve body"}, "downloads": {source}}, hx)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			if !hx && (!strings.Contains(w.Body.String(), "Preserve title") || !strings.Contains(w.Body.String(), "Preserve body")) {
				t.Fatal("form values lost")
			}
		}
	}
	// Actual content wins over the extension or client MIME hint.
	requireStatus(t, uploadTestDownload(t, h, "not-a-pdf.pdf", []byte("plain text")), http.StatusUnprocessableEntity)
	original := h.blog.mediaMetadata
	h.blog.mediaMetadata = failedUploadRecording{original}
	w := uploadTestDownload(t, h, "retry.txt", []byte("completion can retry"))
	requireStatus(t, w, http.StatusInternalServerError)
	id := downloadID([]byte("completion can retry"), ".txt")
	meta, _ := original.GetMediaMetadata(t.Context(), id)
	if meta.UploadedAt != nil || meta.UploadedBy == "" {
		t.Fatal("failed upload falsely completed")
	}
	file, err := h.blog.downloads.Open(t.Context(), id)
	if err != nil {
		t.Fatal("failed recording removed bytes", err)
	}
	file.Close()
	h.blog.mediaMetadata = original
	testDownload(t, h, "retry.txt", "completion can retry")
	meta, _ = original.GetMediaMetadata(t.Context(), id)
	if meta.UploadedAt == nil {
		t.Fatal("retry not recorded")
	}
}

func TestDownloadCleanupAndSearch(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	entry := testDownload(t, h, "notes.txt", "Downloadable text")
	entry.Description = "A searchable attachment description"
	source, err := joinPostComponents("Ordinary writing", "", downloadSource(t, entry), "")
	if err != nil {
		t.Fatal(err)
	}
	post, err := h.blog.persistPost(t.Context(), Post{Title: "With attachment", Markdown: source})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []struct {
		text    string
		matches bool
	}{
		{"attachment description", true}, {"notes.txt", true}, {"Ordinary writing", true},
		{"post-components", false}, {entry.ID, false}, {"downloads", false},
	} {
		result, err := h.blog.searchStore.SearchPublic(t.Context(), query.text)
		if err != nil {
			t.Fatal(err)
		}
		if (len(result.Items) > 0) != query.matches {
			t.Fatal("incorrect component search", query.text, result)
		}
	}
	// Unused files participate in the same reviewed batch deletion as images.
	unused := testDownload(t, h, "unused.txt", "Unused download")
	var selected mediaFile
	files, err := h.blog.unusedMediaFiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.ID == unused.ID {
			selected = file
		}
	}
	if selected.ID == "" {
		t.Fatal("unused download missing from cleanup")
	}
	form, err := parseMediaDeleteBatch([]string{selected.ID + ":" + strconv.FormatInt(selected.Version(), 10)})
	if err != nil {
		t.Fatal("download rejected from cleanup batch", err)
	}
	result, err := h.blog.removeUnusedMediaBatch(t.Context(), form)
	if err != nil || result.Deleted != 1 {
		t.Fatal("download cleanup failed", result, err)
	}
	if _, err := h.blog.downloads.Open(t.Context(), unused.ID); !errors.Is(err, errNotFound) {
		t.Fatal("unused file retained", err)
	}
	if file, err := h.blog.downloads.Open(t.Context(), entry.ID); err != nil {
		t.Fatal("used file removed", err)
	} else {
		file.Close()
	}
	// A scheduled reference becomes public only when its publication time arrives.
	post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	post.Published = time.Now().Add(time.Hour)
	post.Draft = false
	post, err = h.blog.persistPost(t.Context(), post)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, httptest.NewRequest("GET", "https://blog.example/files/"+entry.ID, nil))
	requireStatus(t, w, http.StatusNotFound)
}
