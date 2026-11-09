package blog

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestAlbumMarkdown(t *testing.T) {
	id := strings.Repeat("a", 64) + ".jpeg"
	line := `![A \[photo\] & <script> "caption"](/images/` + id + `)`
	source, err := joinPostAlbum("Before\n\nAfter", line)
	if err != nil {
		t.Fatal(err)
	}
	body, album := splitPostAlbum(source)
	if body != "Before\n\nAfter" || strings.TrimSpace(album) != line {
		t.Fatalf("album did not round trip: %q %q", body, album)
	}
	rendered, err := markdown(source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(rendered)
	if !strings.Contains(output, `class="photo-album-grid"`) || !strings.Contains(output, `?variant=thumbnail`) || !strings.Contains(output, `A [photo] &amp; &lt;script&gt; &#34;caption&#34;`) || strings.Contains(output, `<script>`) {
		t.Fatal("album must render safe captions and thumbnails", output)
	}
	if !strings.Contains(output, `<figure><img`) || !strings.Contains(output, `</figcaption></figure></a>`) {
		t.Fatal("the photo and caption must share one linked figure", output)
	}
	if strings.Index(output, "After") > strings.Index(output, `class="photo-album"`) {
		t.Fatal("album must appear after the writing", output)
	}
	refs, err := imageReferences(source, "/post/s/album", "")
	if err != nil || !slices.Contains(refs, "/images/"+id) {
		t.Fatal("album photos must participate in image tracking", refs, err)
	}
	for _, source := range []string{"~~~\n```photo-album\n" + line + "\n```\n~~~", "> ```photo-album\n> " + line + "\n> ```", "```go\n" + line + "\n```"} {
		body, album := splitPostAlbum(source)
		if body != source || album != "" {
			t.Fatal("ordinary code and quoted examples must remain writing", source)
		}
	}
	base, _ := url.Parse("https://blog.example/post/s/album")
	feed, err := markdownAt(source, base)
	if err != nil || !strings.Contains(string(feed), `href="https://blog.example/images/`+id) {
		t.Fatal("feeds must resolve album links", feed, err)
	}
}

func TestAlbumValidation(t *testing.T) {
	id := strings.Repeat("a", 64) + ".png"
	valid := "![](/images/" + id + ")"
	for name, source := range map[string]string{
		"external":     "![External](https://example.com/photo.png)",
		"unsafe":       "![Unsafe](javascript:alert(1))",
		"attributes":   `![Caption](/images/` + id + `" onerror="alert(1))`,
		"duplicate":    valid + "\n" + valid,
		"caption":      "![" + strings.Repeat("é", 501) + "](/images/" + id + ")",
		"invalid UTF8": "![\xff](/images/" + id + ")",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAlbum(source); err == nil {
				t.Fatal("expected album validation failure")
			}
		})
	}
	var lines []string
	for index := range 51 {
		lines = append(lines, fmt.Sprintf("![](/images/%064x.png)", index))
	}
	if _, err := parseAlbum(strings.Join(lines, "\n")); err == nil {
		t.Fatal("more than 50 photos accepted")
	}
	for _, source := range []string{"", valid, "\r\n" + valid + "\r\n"} {
		if _, err := parseAlbum(source); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{"```go\nUnfinished code", "<script>\nUnfinished HTML", "~~~\nUnfinished code"} {
		if _, err := joinPostAlbum(body, valid); err == nil {
			t.Fatal("an unfinished block swallowed the album", body)
		}
	}
}

func TestAlbumFinalBlock(t *testing.T) {
	var earlier []string
	for index := range maxAlbumPhotos {
		earlier = append(earlier, fmt.Sprintf("![Earlier %d](/images/%064x.png)", index, index))
	}
	body := "Writing\n\n```photo-album\n" + strings.Join(earlier, "\n") + "\n```\n\nFinal paragraph"
	id := strings.Repeat("f", 64) + ".jpeg"
	line := "![Managed photo](/images/" + id + ")"
	source, err := joinPostAlbum(body, line)
	if err != nil {
		t.Fatal(err)
	}
	gotBody, gotAlbum := splitPostAlbum(source)
	if gotBody != body || strings.TrimSpace(gotAlbum) != line {
		t.Fatal("editing must preserve earlier blocks as writing and expose only the final album")
	}
	rendered, err := markdown(source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(rendered), "data-album-photo") != 1 || !strings.Contains(string(rendered), `data-caption="Managed photo"`) || !strings.Contains(string(rendered), "![Earlier 0]") {
		t.Fatal("earlier blocks must stay code without consuming the managed album limit", rendered)
	}
	refs, err := imageReferences(source, "/post/s/album", "")
	if err != nil || !slices.Equal(refs, []string{"/images/" + id}) {
		t.Fatal("only the final album can protect or publish images", refs, err)
	}
	for _, source := range []string{body, "```photo-album\ninvalid album\n```", "> ```photo-album\n> " + line + "\n> ```", "~~~~\n```photo-album\n" + line + "\n```\n~~~~", "[Example][ref]\n\n```photo-album\n" + line + "\n```\n\n[ref]: https://example.com/"} {
		rendered, err := markdown(source)
		if err != nil || strings.Contains(string(rendered), "data-album-photo") {
			t.Fatal("non-final, invalid, or nested albums must remain writing", rendered, err)
		}
	}
	trailing := "[Example][ref]\n\n```photo-album\n" + line + "\n```\n\n[ref]: https://example.com/"
	if body, album := splitPostAlbum(trailing); body != trailing || album != "" {
		t.Fatal("extracting an album must not discard trailing link definitions")
	}
}

func TestAlbumAuthoringAndVisibility(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(fmt.Sprint(hx), func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			photo := mediaUpload(t, h.blog, "Album photo")
			line := "![Album caption](/images/" + photo.ID + ")"
			fields := url.Values{"title": {"Album post"}, "slug": {"album-post"}, "content": {"Post body"}, "album": {line}}
			w := h.request("POST", "/post", fields, hx)
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, w, status)
			post, err := h.blog.posts.GetBySlug(t.Context(), "album-post", true)
			if err != nil || !strings.Contains(post.Markdown, line) {
				t.Fatal("album was not saved", post, err)
			}
			publicRequest := func(path string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				return w
			}
			requireStatus(t, publicRequest("/images/"+photo.ID), http.StatusNotFound)
			if err := h.blog.removeMedia(t.Context(), photo.ID, photo.Version(), true); err != errMediaUsed {
				t.Fatal("draft album did not protect upload", err)
			}
			editor := h.request("GET", "/post/update/"+post.ID, nil, hx).Body.String()
			if !strings.Contains(editor, `required>Post body</textarea>`) || !strings.Contains(editor, line+"\n</textarea>") {
				t.Fatal("editor must separate the body from the album", editor)
			}
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			requireStatus(t, publicRequest("/images/"+photo.ID+"?variant=thumbnail"), http.StatusOK)
			requireStatus(t, publicRequest("/images/"+photo.ID), http.StatusOK)
			for _, partial := range []bool{false, true} {
				w := h.request("GET", "/post/s/album-post", nil, partial)
				requireStatus(t, w, http.StatusOK)
				if !strings.Contains(w.Body.String(), `data-album-photo`) {
					t.Fatal("album missing from post")
				}
			}
			post.Markdown = "No album"
			post, err = h.blog.persistPost(t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			requireStatus(t, publicRequest("/images/"+photo.ID), http.StatusNotFound)
			if err := h.blog.removeMedia(t.Context(), photo.ID, photo.Version(), false); err != errMediaHistory {
				t.Fatal("historical album did not protect upload", err)
			}
			versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
			if err != nil || len(versions) != 2 {
				t.Fatal("album missing from history", versions, err)
			}
			snapshot, err := h.blog.history.GetRevision(t.Context(), "post", post.ID, versions[1].ID)
			if err != nil || !strings.Contains(snapshot.Markdown, line) {
				t.Fatal("saved album content missing", snapshot, err)
			}
		})
	}
}

func TestAlbumFormErrors(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(fmt.Sprint(hx), func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			photo := mediaUpload(t, h.blog, "Album form")
			line := "![Keep caption](/images/" + photo.ID + ")"
			for _, album := range []string{line, "invalid album"} {
				w := h.request("POST", "/post", url.Values{"title": {""}, "content": {"Keep writing"}, "album": {album}}, hx)
				requireStatus(t, w, http.StatusUnprocessableEntity)
				if !hx && (!strings.Contains(w.Body.String(), album+"</textarea>") || !strings.Contains(w.Body.String(), `required>Keep writing</textarea>`)) {
					t.Fatal("ordinary validation lost submitted writing or album", w.Body.String())
				}
			}
			missing := "![](/images/" + strings.Repeat("f", 64) + ".jpeg)"
			w := h.request("POST", "/post", url.Values{"title": {"Missing photo"}, "content": {"Keep writing"}, "album": {missing}}, hx)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			if !strings.Contains(w.Body.String(), errMissingMedia.Error()) {
				t.Fatal("missing album upload was not rejected")
			}
		})
	}
}
