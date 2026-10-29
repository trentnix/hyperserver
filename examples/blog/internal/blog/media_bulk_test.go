package blog

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// mediaBatchForm reproduces the fixed file versions embedded in a confirmation.
func mediaBatchForm(files ...mediaFile) url.Values {
	values := url.Values{}
	for _, file := range files {
		values.Add("file", file.ID+":"+strconv.FormatInt(file.Version(), 10))
	}
	return values
}

// TestMediaBulkHTTP checks the reviewed snapshot, protected files, and safe replay.
func TestMediaBulkHTTP(t *testing.T) {
	h := newHarness(t)
	first := mediaUpload(t, h.blog, "first")
	used := mediaUpload(t, h.blog, "used later")
	changed := mediaUpload(t, h.blog, "uploaded again")
	missing := mediaUpload(t, h.blog, "removed separately")
	const path = "/media/delete-unused"
	for _, state := range []string{"anonymous", "unverified"} {
		if state == "unverified" {
			h.register(t)
			h.login(t)
		}
		for _, method := range []string{"GET", "POST"} {
			requireStatus(t, h.request(method, path, mediaBatchForm(first), false), http.StatusForbidden)
		}
	}
	h.verify(t)
	confirmation := h.request("GET", path, nil, false)
	requireStatus(t, confirmation, http.StatusOK)
	if confirmation.Header().Get("Cache-Control") != "no-store" || !strings.Contains(confirmation.Body.String(), "4 images") || !strings.Contains(confirmation.Body.String(), "bytes") {
		t.Fatal("missing confirmation summary or privacy")
	}
	values := url.Values{}
	for _, match := range regexp.MustCompile(`name="file" value="([^"]+)"`).FindAllStringSubmatch(confirmation.Body.String(), -1) {
		values.Add("file", html.UnescapeString(match[1]))
	}
	if len(values["file"]) != 4 {
		t.Fatal("missing snapshot", values)
	}
	newUpload := mediaUpload(t, h.blog, "new after confirmation")
	if err := h.blog.writeProfile(t.Context(), Profile{Name: "Saved", ImageURL: "/images/" + used.ID}); err != nil {
		t.Fatal(err)
	}
	when := changed.Modified.Add(time.Second)
	if err := os.Chtimes(filepath.Join(h.blog.images.(*fileImageStore).root.Name(), changed.ID), when, when); err != nil {
		t.Fatal(err)
	}
	if err := h.blog.removeMedia(t.Context(), missing.ID, missing.Version(), false); err != nil {
		t.Fatal(err)
	}

	// Cross-site requests cannot trigger a reviewed batch.
	r := httptest.NewRequest("POST", "https://blog.example"+path, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://attacker.example")
	for _, cookie := range h.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.app.Handler.ServeHTTP(w, r)
	requireStatus(t, w, http.StatusForbidden)
	w = h.request("POST", path, values, false)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Deleted: 1. Skipped: 3. Not processed: 0.") {
		t.Fatal(w.Body.String())
	}
	for _, file := range []mediaFile{used, changed, newUpload} {
		image, err := h.blog.images.Open(t.Context(), file.ID)
		if err != nil {
			t.Fatal("protected upload removed", err)
		}
		image.Content.Close()
	}
	if _, err := h.blog.images.Open(t.Context(), first.ID); !errors.Is(err, errNotFound) {
		t.Fatal("unused image remains", err)
	}
	w = h.request("POST", path, values, false)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Deleted: 0. Skipped: 4. Not processed: 0.") {
		t.Fatal("unsafe replay", w.Body.String())
	}
}

// TestMediaBulkValidation rejects the whole form before touching any uploads.
func TestMediaBulkValidation(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	f := mediaUpload(t, h.blog, "keep")
	valid := mediaBatchForm(f)["file"][0]
	for _, values := range [][]string{nil, {valid, valid}, {valid, "bad"}, {valid, "../image.png:1"}, {valid, f.ID + ":bad"}, {valid, f.ID + ":9223372036854775808"}, make([]string, maxMediaDeleteBatch+1)} {
		if _, err := parseMediaDeleteBatch(values); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	w := h.request("POST", "/media/delete-unused", url.Values{"file": {valid, "bad"}}, false)
	requireStatus(t, w, http.StatusBadRequest)
	image, err := h.blog.images.Open(t.Context(), f.ID)
	if err != nil {
		t.Fatal("invalid batch deleted a file", err)
	}
	image.Content.Close()
	files, err := parseMediaDeleteBatch([]string{valid})
	if err != nil || len(files) != 1 || files[0].ID != f.ID || files[0].Version() != f.Version() {
		t.Fatal(files, err)
	}
}

type failingBulkRemove struct {
	mediaStore
	failID string
}

func (s failingBulkRemove) Remove(ctx context.Context, id string, version int64) error {
	if id == s.failID {
		return errors.New("private storage failure")
	}
	return s.mediaStore.Remove(ctx, id, version)
}

// TestMediaBulkFailure reports completed work and stops on the first unexpected error.
func TestMediaBulkFailure(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	a, b, c := mediaUpload(t, h.blog, "a"), mediaUpload(t, h.blog, "b"), mediaUpload(t, h.blog, "c")
	h.blog.media = failingBulkRemove{h.blog.media, b.ID}
	w := h.request("POST", "/media/delete-unused", mediaBatchForm(a, b, c), false)
	requireStatus(t, w, http.StatusInternalServerError)
	if !strings.Contains(w.Body.String(), "Deleted: 1. Skipped: 0. Not processed: 2.") || strings.Contains(w.Body.String(), "private storage failure") {
		t.Fatal(w.Body.String())
	}
	if _, err := h.blog.images.Open(t.Context(), a.ID); !errors.Is(err, errNotFound) {
		t.Fatal(err)
	}
	for _, f := range []mediaFile{b, c} {
		image, err := h.blog.images.Open(t.Context(), f.ID)
		if err != nil {
			t.Fatal(err)
		}
		image.Content.Close()
	}
	h.blog.mediaReferences = failedMediaReferences{}
	result, err := h.blog.removeUnusedMediaBatch(t.Context(), []mediaFile{c})
	if err == nil || result != (mediaDeleteResult{Remaining: 1}) {
		t.Fatal(result, err)
	}
	requireStatus(t, h.request("GET", "/media/delete-unused", nil, false), http.StatusInternalServerError)
}

// TestMediaBulkConcurrentSave keeps a batch from deleting a concurrently saved profile image.
func TestMediaBulkConcurrentSave(t *testing.T) {
	h := newHarness(t)
	f := mediaUpload(t, h.blog, "concurrent bulk")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	h.blog.profileStore = blockedProfileSave{h.blog.profileStore, entered, release}
	saved := make(chan error, 1)
	go func() { saved <- h.blog.writeProfile(ctx, Profile{Name: "Saved", ImageURL: "/images/" + f.ID}) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	deleted := make(chan mediaDeleteResult, 1)
	removeErrors := make(chan error, 1)
	go func() {
		result, err := h.blog.removeUnusedMediaBatch(ctx, []mediaFile{f})
		deleted <- result
		removeErrors <- err
	}()
	close(release)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	if result := <-deleted; result != (mediaDeleteResult{Skipped: 1}) {
		t.Fatal(result)
	}
	if err := <-removeErrors; err != nil {
		t.Fatal(err)
	}
}

type listedMedia struct {
	mediaStore
	files []mediaFile
}

func (s listedMedia) List(context.Context) ([]mediaFile, error) { return s.files, nil }

// TestMediaBulkConfirmationBounds makes large batches explicit and hides empty destructive actions.
func TestMediaBulkConfirmationBounds(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	w := h.request("GET", "/media/delete-unused", nil, false)
	requireStatus(t, w, http.StatusOK)
	if strings.Contains(w.Body.String(), `method="post" action="/media/delete-unused"`) {
		t.Fatal("empty confirmation has a delete form")
	}
	f := mediaUpload(t, h.blog, "batch size")
	f.Size = 123
	files := make([]mediaFile, maxMediaDeleteBatch+1)
	for i := range files {
		files[i] = f
		files[i].ID = fmt.Sprintf("%064x.png", i+1)
	}
	h.blog.media = listedMedia{h.blog.media, files}
	w = h.request("GET", "/media/delete-unused", nil, false)
	requireStatus(t, w, http.StatusOK)
	if strings.Count(w.Body.String(), `name="file"`) != maxMediaDeleteBatch || !strings.Contains(w.Body.String(), "Additional images outside this batch: 1.") || !strings.Contains(w.Body.String(), strconv.Itoa(maxMediaDeleteBatch*123)+" bytes") {
		t.Fatal("incorrect bounded confirmation")
	}
	// The largest confirmation must fit both form parsing and the route body limit.
	w = h.request("POST", "/media/delete-unused", mediaBatchForm(files[:maxMediaDeleteBatch]...), false)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Deleted: 0. Skipped: 5000. Not processed: 0.") {
		t.Fatal("maximum batch was not processed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := h.blog.removeUnusedMediaBatch(ctx, []mediaFile{f})
	if !errors.Is(err, context.Canceled) || result != (mediaDeleteResult{Remaining: 1}) {
		t.Fatal(result, err)
	}
}
