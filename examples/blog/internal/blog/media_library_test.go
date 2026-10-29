package blog

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMediaDetail exposes exact saved uses only to the verified administrator.
func TestMediaDetail(t *testing.T) {
	h := newHarness(t)
	f := mediaUpload(t, h.blog, "details")
	path := "/media/" + f.ID
	requireStatus(t, h.request("GET", path, nil, false), http.StatusForbidden)
	h.register(t)
	h.login(t)
	requireStatus(t, h.request("GET", path, nil, false), http.StatusForbidden)
	h.verify(t)
	w := h.request("GET", path, nil, false)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Not used by saved posts") || !strings.Contains(w.Body.String(), "/media/delete/"+f.ID) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unused upload must show its status and offer deletion privately")
	}
	p, err := h.blog.posts.Save(t.Context(), Post{Title: "Draft <post>", Slug: "media-detail", Markdown: "![Image](/images/" + f.ID + ")", PreviewImage: f.ID, Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	page, err := h.blog.pageStore.SavePage(t.Context(), Page{Title: "Draft page", Slug: "media-detail", Markdown: "Text", PreviewImage: f.ID})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("POST", "/profile", url.Values{"name": {"Profile"}, "imageURL": {"/images/" + f.ID}}, false), http.StatusSeeOther)
	used, err := h.blog.mediaReferences.ReferencedImages(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	content := used[f.ID].Content
	for _, want := range []mediaReference{{URL: "/post/update/" + p.ID, Title: p.Title}, {URL: "/pages/update/" + page.ID, Title: page.Title}, {URL: "/profile", Title: "Site portrait"}} {
		if !slices.Contains(content, want) {
			t.Fatalf("missing reference %v in %v", want, content)
		}
	}
	if len(content) != 3 {
		t.Fatal("body and preview must not duplicate an owner", content)
	}
	w = h.request("GET", path, nil, false)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Draft &lt;post&gt;") || !strings.Contains(w.Body.String(), "/history/post/"+p.ID) || strings.Contains(w.Body.String(), "/media/delete/"+f.ID) {
		t.Fatal("details must escape titles, link history, and protect current uses")
	}
	p.Markdown, p.PreviewImage = "No image", ""
	if _, err := h.blog.posts.Save(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	used, err = h.blog.mediaReferences.ReferencedImages(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(used[f.ID].Content) != 2 || len(used[f.ID].History) == 0 {
		t.Fatal("editing must remove the current use but retain history")
	}
	requireStatus(t, h.request("GET", "/media/not-an-image", nil, false), http.StatusNotFound)
	requireStatus(t, h.request("GET", "/media/"+strings.Repeat("0", 64)+".png", nil, false), http.StatusNotFound)
	h.blog.mediaReferences = failedMediaReferences{}
	requireStatus(t, h.request("GET", path, nil, false), http.StatusInternalServerError)
}

// TestMediaLibrary protects private uploads and bounds both response variants.
func TestMediaLibrary(t *testing.T) {
	h := newHarness(t)
	f := mediaUpload(t, h.blog, "private library image")
	for _, state := range []string{"anonymous", "unverified"} {
		if state == "unverified" {
			h.register(t)
			h.login(t)
		}
		for _, path := range []string{"/media", "/media?pick=1"} {
			w := h.request("GET", path, nil, false)
			requireStatus(t, w, http.StatusForbidden)
			if strings.Contains(w.Body.String(), f.ID) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("library leaked private uploads or allowed caching")
			}
		}
	}
	h.verify(t)
	for _, hx := range []bool{false, true} {
		w := h.request("GET", "/media?pick=1", nil, hx)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), `data-media-select="`+f.ID+`"`) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing selection or private cache policy")
		}
	}
	files := make([]mediaFile, mediaPageSize+1)
	for i := range files {
		files[i] = mediaFile{ID: fmt.Sprintf("%064x.png", len(files)-i), Modified: time.Unix(100, 0)}
	}
	h.blog.media = listedMedia{mediaStore: h.blog.media, files: files}
	ids := regexp.MustCompile(`data-media-select="([^"]+)"`)
	w := h.request("GET", "/media?pick=1", nil, false)
	requireStatus(t, w, http.StatusOK)
	selected := ids.FindAllStringSubmatch(w.Body.String(), -1)
	if len(selected) != mediaPageSize || selected[0][1] != fmt.Sprintf("%064x.png", 1) || !strings.Contains(w.Body.String(), `data-media-page="/media?page=2&amp;pick=1"`) {
		t.Fatal("incorrect page size, deterministic tie order, or next link")
	}
	w = h.request("GET", "/media?page=2&pick=1", nil, false)
	selected = ids.FindAllStringSubmatch(w.Body.String(), -1)
	if len(selected) != 1 || selected[0][1] != fmt.Sprintf("%064x.png", len(files)) || !strings.Contains(w.Body.String(), `data-media-page="/media?page=1&amp;pick=1"`) {
		t.Fatal("incorrect final page or previous link")
	}
	newest := files[len(files)-1].ID
	files[len(files)-1].Modified = time.Unix(200, 0)
	w = h.request("GET", "/media?pick=1", nil, false)
	if ids.FindAllStringSubmatch(w.Body.String(), -1)[0][1] != newest {
		t.Fatal("newest upload did not sort first")
	}
	for _, page := range []string{"0", "-1", "abc", "999999999999999999999999999"} {
		requireStatus(t, h.request("GET", "/media?page="+page, nil, false), http.StatusBadRequest)
	}
	for _, page := range []string{"3", "9223372036854775807"} {
		requireStatus(t, h.request("GET", "/media?page="+page, nil, false), http.StatusNotFound)
	}
	h.blog.media = listedMedia{files: nil}
	w = h.request("GET", "/media", nil, false)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "No uploaded images.") || strings.Contains(w.Body.String(), "data-media-select=") {
		t.Fatal("incorrect empty library")
	}
	h.blog.media = failedMediaList{}
	w = h.request("GET", "/media", nil, false)
	requireStatus(t, w, http.StatusInternalServerError)
	if strings.Contains(w.Body.String(), "private list failure") {
		t.Fatal("storage error exposed")
	}
}

// TestMediaReuse saves the same existing upload across consumers without duplicating it.
func TestMediaReuse(t *testing.T) {
	h := newHarness(t)
	f := mediaUpload(t, h.blog, "reused")
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, endpoint := range []string{"/post", "/pages"} {
		form := url.Values{"title": {"Reused image"}, "slug": {"reuse"}, "content": {"![Image](/images/" + f.ID + ")"}, "previewImage": {f.ID}, "childrenDisplay": {"hidden"}}
		requireStatus(t, h.request("POST", endpoint, form, false), http.StatusSeeOther)
	}
	cookies := h.cookies
	h.cookies = nil
	requireStatus(t, h.request("GET", "/images/"+f.ID, nil, false), http.StatusNotFound)
	h.cookies = cookies
	requireStatus(t, h.request("POST", "/profile", url.Values{"name": {"Profile"}, "imageURL": {"/images/" + f.ID}, "faviconURL": {"/images/" + f.ID}, "backgroundURL": {"/images/" + f.ID}}, false), http.StatusSeeOther)
	h.cookies = nil
	requireStatus(t, h.request("GET", "/images/"+f.ID, nil, false), http.StatusOK)
	files, err := h.blog.media.List(t.Context())
	if err != nil || len(files) != 1 || files[0].ID != f.ID || files[0].Version() != f.Version() {
		t.Fatal("reuse rewrote or duplicated the upload", files, err)
	}
}
