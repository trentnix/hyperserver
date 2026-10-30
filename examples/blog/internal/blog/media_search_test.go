package blog

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMediaMetadata checks persistence, duplicate uploads, and stale label protection.
func TestMediaMetadata(t *testing.T) {
	settings := Settings{}
	settings.Storage.Provider, settings.Storage.Connection = "sqlite", "media.db"
	directory := t.TempDir()
	store, closeStore, err := openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 64) + ".png"
	missing, err := store.GetMediaMetadata(t.Context(), id)
	if err != nil || missing != (mediaMetadata{}) {
		t.Fatal(missing, err)
	}
	if err := store.RememberMedia(t.Context(), id, "First.png"); err != nil {
		t.Fatal(err)
	}
	if err := store.RenameMedia(t.Context(), id, "Custom label", 1); err != nil {
		t.Fatal(err)
	}
	if err := store.RememberMedia(t.Context(), id, "Second.png"); err != nil {
		t.Fatal(err)
	}
	if err := closeStore(); err != nil {
		t.Fatal(err)
	}
	store, closeStore, err = openRepository(t.Context(), settings, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	got, err := store.GetMediaMetadata(t.Context(), id)
	if err != nil || got != (mediaMetadata{Filename: "First.png", Label: "Custom label", Version: 2}) {
		t.Fatal(got, err)
	}
	for _, version := range []int64{0, 1} {
		if err := store.RenameMedia(t.Context(), id, "Stale label", version); !errors.Is(err, errEditConflict) {
			t.Fatal(err)
		}
	}
	legacy := strings.Repeat("b", 64) + ".png"
	if err := store.RenameMedia(t.Context(), legacy, "Legacy label", 0); err != nil {
		t.Fatal(err)
	}
	if err := store.RememberMedia(t.Context(), legacy, "Recovered.png"); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetMediaMetadata(t.Context(), legacy)
	if err != nil || got.Filename != "Recovered.png" || got.Label != "Legacy label" || got.Version != 2 {
		t.Fatal(got, err)
	}

	results := make(chan error, 4)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() { results <- store.RenameMedia(t.Context(), id, "Concurrent", 2) })
	}
	workers.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, errEditConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent winners = %d", winners)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ListMediaMetadata(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := store.GetMediaMetadata(ctx, id); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.RememberMedia(ctx, id, "canceled"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.RenameMedia(ctx, id, "canceled", 3); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// TestMediaQuery checks literal and Unicode matching and mutually exclusive usage groups.
func TestMediaQuery(t *testing.T) {
	file := mediaFile{ID: strings.Repeat("a", 64) + ".png"}
	metadata := mediaMetadata{Filename: "Straße.png", Label: "100%_ÉTÉ"}
	for _, query := range []string{"", "strasse", "été", "%_", ".png"} {
		if !(mediaQuery{Search: query}).matches(file, metadata, mediaUsage{}) {
			t.Fatalf("did not match %q", query)
		}
	}
	if (mediaQuery{Search: "x%_"}).matches(file, metadata, mediaUsage{}) {
		t.Fatal("wildcards were interpreted")
	}
	for _, tc := range []struct {
		usage mediaUsage
		want  string
	}{
		{mediaUsage{}, "unused"},
		{mediaUsage{Current: true}, "current"},
		{mediaUsage{History: []mediaRevision{{Title: "Older title"}}}, "history"},
		{mediaUsage{Current: true, History: []mediaRevision{{Title: "Older title"}}}, "current"},
	} {
		for _, filter := range []string{"current", "history", "unused"} {
			if got := (mediaQuery{Usage: filter}).matches(file, metadata, tc.usage); got != (filter == tc.want) {
				t.Fatalf("usage %s: %v", filter, tc)
			}
		}
	}
}

// TestMediaSearchHTTP checks real references, query validation, and filtering before pagination.
func TestMediaSearchHTTP(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	current := mediaUpload(t, h.blog, "current")
	historical := mediaUpload(t, h.blog, "historical")
	unused := mediaUpload(t, h.blog, "unused")
	if err := h.blog.mediaMetadata.RememberMedia(t.Context(), unused.ID, "100%_Été.png"); err != nil {
		t.Fatal(err)
	}
	post, err := h.blog.posts.Save(t.Context(), Post{Slug: "references", Title: "Old forest", Draft: true, PreviewImage: historical.ID})
	if err != nil {
		t.Fatal(err)
	}
	post.Title, post.PreviewImage = "New mountain", current.ID
	if _, err := h.blog.posts.Save(t.Context(), post); err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`data-media-select="([^"]+)"`)
	for _, tc := range []struct{ query, want string }{
		{"q=OLD+FOREST", historical.ID}, {"q=mountain", current.ID},
		{"q=" + url.QueryEscape("%_été"), unused.ID}, {"usage=unused", unused.ID},
		{"usage=history", historical.ID}, {"usage=current", current.ID},
		{"q=no-match", ""},
	} {
		w := h.request("GET", "/media?pick=1&"+tc.query, nil, false)
		requireStatus(t, w, http.StatusOK)
		matches := ids.FindAllStringSubmatch(w.Body.String(), -1)
		if tc.want == "" {
			if len(matches) != 0 || !strings.Contains(w.Body.String(), "No images match") {
				t.Fatal("incorrect empty result", tc.query)
			}
		} else if len(matches) != 1 || matches[0][1] != tc.want {
			t.Fatal("incorrect result", tc.query, matches)
		}
	}
	for _, query := range []string{"usage=bad", "q=" + strings.Repeat("x", 241), "q=%FF"} {
		requireStatus(t, h.request("GET", "/media?"+query, nil, false), http.StatusBadRequest)
	}

	var files []mediaFile
	for i := range 50 {
		id := fmt.Sprintf("%064x.png", i)
		files = append(files, mediaFile{ID: id, Modified: time.Unix(int64(i), 0)})
		name := "Other"
		if i < 25 {
			name = "Needle & thread"
		}
		if err := h.blog.mediaMetadata.RememberMedia(t.Context(), id, name); err != nil {
			t.Fatal(err)
		}
	}
	h.blog.media = listedMedia{files: files}
	w := h.request("GET", "/media?pick=1&q=Needle+%26+thread&usage=unused", nil, true)
	requireStatus(t, w, http.StatusOK)
	if len(ids.FindAllStringSubmatch(w.Body.String(), -1)) != 24 || !strings.Contains(w.Body.String(), "25 images found") {
		t.Fatal("filter applied after pagination")
	}
	link := regexp.MustCompile(`data-media-page="([^"]+)"`).FindAllStringSubmatch(w.Body.String(), -1)
	next := html.UnescapeString(link[len(link)-1][1])
	u, err := url.Parse(next)
	if err != nil || u.Query().Get("q") != "Needle & thread" || u.Query().Get("usage") != "unused" || u.Query().Get("pick") != "1" {
		t.Fatal("pagination lost filters", next, err)
	}
	w = h.request("GET", next, nil, false)
	requireStatus(t, w, http.StatusOK)
	if len(ids.FindAllStringSubmatch(w.Body.String(), -1)) != 1 {
		t.Fatal("wrong final page")
	}
}

// TestMediaRename protects private metadata, escapes text, and preserves stale edits for review.
func TestMediaRename(t *testing.T) {
	h := newHarness(t)
	file := mediaUpload(t, h.blog, "rename")
	path := "/media/" + file.ID
	values := url.Values{"label": {"<New label>"}, "version": {"0"}}
	requireStatus(t, h.request("POST", path, values, false), http.StatusForbidden)
	h.register(t)
	h.login(t)
	requireStatus(t, h.request("POST", path, values, false), http.StatusForbidden)
	h.verify(t)
	requireStatus(t, h.request("POST", path, values, false), http.StatusSeeOther)
	w := h.request("GET", path, nil, false)
	if !strings.Contains(w.Body.String(), "&lt;New label&gt;") || strings.Contains(w.Body.String(), "value=\"<New label>") {
		t.Fatal("label was not escaped")
	}
	w = h.request("POST", path, values, false)
	requireStatus(t, w, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "another window") || !strings.Contains(w.Body.String(), "&lt;New label&gt;") {
		t.Fatal("conflict discarded label")
	}
	for _, label := range []string{strings.Repeat("é", 241), "bad\x00name", string([]byte{0xff})} {
		requireStatus(t, h.request("POST", path, url.Values{"label": {label}, "version": {"1"}}, false), http.StatusUnprocessableEntity)
	}
	requireStatus(t, h.request("POST", path, url.Values{"version": {"-1"}}, false), http.StatusBadRequest)
	requireStatus(t, h.request("POST", "/media/"+strings.Repeat("0", 64)+".png", values, false), http.StatusNotFound)
	requireStatus(t, h.request("POST", path, url.Values{"label": {""}, "version": {"1"}}, false), http.StatusSeeOther)
	files, err := h.blog.media.List(t.Context())
	if err != nil || len(files) != 1 || files[0].Version() != file.Version() {
		t.Fatal("renaming changed image bytes or cleanup version", err)
	}
}

func TestUploadFilename(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"/tmp/photo.png", "photo.png"}, {`C:\fakepath\photo.png`, "photo.png"},
		{"photo\n.png", "photo.png"}, {"", ""}, {"/", ""},
		{strings.Repeat("é", 250) + ".png", strings.Repeat("é", 240)},
	} {
		if got := uploadFilename(tc.input); got != tc.want {
			t.Fatalf("%q: %q", tc.input, got)
		}
	}
}

type failedMediaMetadata struct{}

func (failedMediaMetadata) ListMediaMetadata(context.Context) (map[string]mediaMetadata, error) {
	return nil, errors.New("private metadata failure")
}
func (failedMediaMetadata) GetMediaMetadata(context.Context, string) (mediaMetadata, error) {
	return mediaMetadata{}, errors.New("private metadata failure")
}
func (failedMediaMetadata) RememberMedia(context.Context, string, string) error {
	return errors.New("private metadata failure")
}
func (failedMediaMetadata) RenameMedia(context.Context, string, string, int64) error {
	return errors.New("private metadata failure")
}

// TestMediaSearchFailures never turns a failed lookup into an empty result or successful rename.
func TestMediaSearchFailures(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	file := mediaUpload(t, h.blog, "failure")
	metadata := h.blog.mediaMetadata
	h.blog.mediaMetadata = failedMediaMetadata{}
	for _, path := range []string{"/media", "/media?pick=1&q=image", "/media/" + file.ID} {
		w := h.request("GET", path, nil, false)
		requireStatus(t, w, http.StatusInternalServerError)
		if strings.Contains(w.Body.String(), "private metadata") || strings.Contains(w.Body.String(), "No images match") {
			t.Fatal("metadata failure was misrepresented")
		}
	}
	requireStatus(t, h.request("POST", "/media/"+file.ID, url.Values{"label": {"Label"}, "version": {"0"}}, false), http.StatusInternalServerError)
	h.blog.mediaMetadata = metadata
	h.blog.mediaReferences = failedMediaReferences{}
	for _, query := range []string{"q=image", "usage=unused", "usage=current", "usage=history"} {
		requireStatus(t, h.request("GET", "/media?"+query, nil, false), http.StatusInternalServerError)
	}
}
