package blog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestMediaTags checks validation, optional suggestions, privacy, and stale edits.
func TestMediaTags(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	file := mediaUpload(t, h.blog, "tagged")
	path := "/media/" + file.ID
	save := func(tags, version string) int {
		return h.request("POST", path, url.Values{"label": {"Label"}, "tags": {tags}, "version": {version}}, false).Code
	}
	if status := save(" Forest, FOREST, École, E\u0301cole ", "0"); status != http.StatusSeeOther {
		t.Fatal(status)
	}
	metadata, err := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), file.ID)
	if err != nil || metadata.Tags != "forest, école" || metadata.Version != 1 {
		t.Fatal(metadata, err)
	}
	for _, invalid := range []string{strings.Repeat("a", 51), "a\nb", strings.Repeat("a", 4097)} {
		w := h.request("POST", path, url.Values{"tags": {invalid}, "version": {"1"}}, false)
		requireStatus(t, w, http.StatusUnprocessableEntity)
		if !strings.Contains(w.Body.String(), `data-error-fields="tags"`) {
			t.Fatal("invalid tags must identify their control")
		}
	}
	if status := save("stale", "0"); status != http.StatusConflict {
		t.Fatal(status)
	}
	unchanged, err := h.blog.mediaMetadata.GetMediaMetadata(t.Context(), file.ID)
	if err != nil || unchanged != metadata {
		t.Fatal("invalid or stale submission changed metadata", unchanged, err)
	}

	if _, err := h.blog.persistPost(t.Context(), Post{Title: "Writing", Markdown: "Body", Tags: "writing-only", Draft: true}); err != nil {
		t.Fatal(err)
	}
	if err := h.blog.mediaMetadata.SaveMediaMetadata(t.Context(), "deleted.png", mediaMetadata{Tags: "deleted-only"}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", path, nil).WithContext(t.Context())
	if got := h.blog.mediaTagSuggestions(r); !reflect.DeepEqual(got, []string{"forest", "école"}) {
		t.Fatal("suggestions include writing or missing uploads", got)
	}
	if got, err := h.blog.tags.ListTags(t.Context()); err != nil || !reflect.DeepEqual(got, []string{"writing-only"}) {
		t.Fatal("writing suggestions include media", got, err)
	}
	w := h.request("GET", path, nil, false)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "data-tag=\"forest\"") || !strings.Contains(w.Body.String(), "Save details") {
		t.Fatal("image details omitted tags or save action")
	}
	files, err := h.blog.media.List(t.Context())
	if err != nil || len(files) != 1 || files[0].Version() != file.Version() {
		t.Fatal("tags changed file bytes or cleanup version", err)
	}
	used, err := h.blog.mediaReferences.ReferencedImages(t.Context())
	if err != nil || used[file.ID].Current || len(used[file.ID].History) != 0 {
		t.Fatal("tags changed image references", used, err)
	}
	if status := save("", "1"); status != http.StatusSeeOther {
		t.Fatal(status)
	}
	metadata, err = h.blog.mediaMetadata.GetMediaMetadata(t.Context(), file.ID)
	if err != nil || metadata.Tags != "" || metadata.Version != 2 {
		t.Fatal("tags were not cleared", metadata, err)
	}
	requireStatus(t, h.request("POST", "/auth/logout", nil, false), http.StatusSeeOther)
	requireStatus(t, h.request("GET", "/images/"+file.ID, nil, false), http.StatusNotFound)
}

type failedMediaTagSuggestions struct{ mediaMetadataRepository }

func (failedMediaTagSuggestions) ListMediaMetadata(context.Context, ...string) (map[string]mediaMetadata, error) {
	return nil, errors.New("private suggestion failure")
}

// TestMediaTagSuggestionFailures keeps optional lookups from blocking edits or masking validation.
func TestMediaTagSuggestionFailures(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	file := mediaUpload(t, h.blog, "suggestion-failure")
	metadata, media := h.blog.mediaMetadata, h.blog.media
	for _, failure := range []string{"metadata", "files"} {
		h.blog.mediaMetadata, h.blog.media = metadata, media
		if failure == "metadata" {
			h.blog.mediaMetadata = failedMediaTagSuggestions{metadata}
		} else {
			h.blog.media = failedMediaList{media}
		}
		path := "/media/" + file.ID
		requireStatus(t, h.request("GET", path, nil, false), http.StatusOK)
		w := h.request("POST", path, url.Values{"tags": {strings.Repeat("x", 51)}, "version": {"0"}}, false)
		requireStatus(t, w, http.StatusUnprocessableEntity)
		if !strings.Contains(w.Body.String(), "50 characters") || strings.Contains(w.Body.String(), "private suggestion failure") {
			t.Fatal("suggestion failure hid validation or leaked an internal error")
		}
	}
}

// TestMediaTagRanking checks both browsing views and stable ordering within each rank.
func TestMediaTagRanking(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	name := mediaUpload(t, h.blog, "name-match")
	tag := mediaUpload(t, h.blog, "tag-match")
	title := mediaUpload(t, h.blog, "title-match")
	secondTag := mediaUpload(t, h.blog, "second-tag-match")
	for _, entry := range []struct {
		file mediaFile
		data mediaMetadata
	}{
		{name, mediaMetadata{Label: "École photo"}},
		{tag, mediaMetadata{Tags: "école"}},
		{secondTag, mediaMetadata{Tags: "école"}},
	} {
		if err := h.blog.mediaMetadata.SaveMediaMetadata(t.Context(), entry.file.ID, entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.blog.persistPost(t.Context(), Post{Title: "École post", Markdown: "Body", Draft: true, PreviewImage: title.ID}); err != nil {
		t.Fatal(err)
	}
	// Reverse relevance by age to ensure text rank wins over recency.
	name.Modified, tag.Modified = time.Unix(1, 0), time.Unix(2, 0)
	secondTag.Modified, title.Modified = time.Unix(3, 0), time.Unix(4, 0)
	h.blog.media = listedMedia{files: []mediaFile{name, tag, title, secondTag}}
	for _, picking := range []bool{false, true} {
		path := "/media?q=" + url.QueryEscape("E\u0301COLE")
		pattern := `class="media-card" href="/media/([^"]+)"`
		if picking {
			path += "&pick=1"
			pattern = `data-media-select="([^"]+)"`
		}
		w := h.request("GET", path, nil, true)
		requireStatus(t, w, http.StatusOK)
		matches := regexp.MustCompile(pattern).FindAllStringSubmatch(w.Body.String(), -1)
		var got []string
		for _, match := range matches {
			got = append(got, match[1])
		}
		if !reflect.DeepEqual(got, []string{name.ID, secondTag.ID, tag.ID, title.ID}) {
			t.Fatalf("picking=%v: incorrect rank %v", picking, got)
		}
		if !strings.Contains(w.Body.String(), "Best matches first.") {
			t.Fatal("search order description is inaccurate")
		}
	}
}
