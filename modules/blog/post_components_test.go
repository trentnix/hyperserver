package blog

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestPostComponentsRoundTrip(t *testing.T) {
	photo := strings.Repeat("a", 64) + ".jpeg"
	file := postDownload{ID: strings.Repeat("b", 64) + ".pdf", Label: `A <script> & "guide"`, Description: "Long description\nwith another line", Size: 1024}
	album := `![Caption \[brackets\] \\ slash](/images/` + photo + `)`
	source, err := joinPostComponents("Writing before components", album, downloadSource(t, file), "")
	if err != nil {
		t.Fatal(err)
	}
	page := editorPage{Post: Post{Markdown: source}}
	if page.BodyMarkdown() != "Writing before components" || strings.TrimSpace(page.AlbumMarkdown()) != album {
		t.Fatal("component source changed", page.BodyMarkdown(), page.AlbumMarkdown())
	}
	files, err := parseDownloads(page.DownloadsJSON())
	if err != nil || len(files) != 1 || files[0] != file {
		t.Fatal("download changed", files, err)
	}
	rendered, err := markdown(source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "&lt;script&gt;") || strings.Contains(string(rendered), "<script>") || !strings.Contains(string(rendered), `hx-boost="false" download`) {
		t.Fatal("unsafe download markup", rendered)
	}
	refs, err := contentMediaReferences(source, "/post/s/round-trip", "")
	if err != nil || len(refs) != 2 {
		t.Fatal("component reference extraction", refs, err)
	}
	// Removing downloads converts the album back to the existing readable format.
	legacy, err := joinPostComponents(page.BodyMarkdown(), page.AlbumMarkdown(), "", "")
	if err != nil || !strings.Contains(legacy, "```photo-album") || strings.Contains(legacy, "post-components") {
		t.Fatal("legacy album regression", legacy, err)
	}
}

func TestComponentOrderFormsAndHistory(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			file := testDownload(t, h, "ordered.txt", "Component order survives history")
			photo := mediaUpload(t, h.blog, "Ordered photo")
			fields := url.Values{"title": {"Ordered components"}, "slug": {"ordered-components"}, "content": {"Body"}, "album": {"![Photo](/images/" + photo.ID + ")"}, "downloads": {downloadSource(t, file)}, "componentOrder": {`["file-downloads","photo-album"]`}}
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, h.request("POST", "/post", fields, hx), status)
			post, err := h.blog.posts.GetBySlug(t.Context(), "ordered-components", true)
			if err != nil {
				t.Fatal(err)
			}
			check := func(writing string, order []string) {
				t.Helper()
				_, components, err := splitPostComponents(writing)
				if err != nil || !slices.Equal(components.Order(), order) {
					t.Fatal("saved order changed", components.Order(), err)
				}
			}
			check(post.Markdown, []string{"file-downloads", "photo-album"})
			versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
			if err != nil {
				t.Fatal(err)
			}
			original := versions[0]
			// Invalid writing retains both the component values and submitted order.
			fields.Set("title", "")
			w := h.request("POST", "/post", fields, false)
			requireStatus(t, w, http.StatusUnprocessableEntity)
			matches := regexp.MustCompile(`name="componentOrder" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
			if len(matches) != 2 || html.UnescapeString(matches[1]) != fields.Get("componentOrder") {
				t.Fatal("validation discarded component order")
			}
			if strings.Index(w.Body.String(), `data-post-component="file-downloads"`) > strings.Index(w.Body.String(), `data-post-component="photo-album"`) {
				t.Fatal("ordinary editor changed order")
			}
			fields.Set("title", "Ordered components")
			fields.Set("componentOrder", `["photo-album","file-downloads"]`)
			requireStatus(t, h.submitEditor(t, "/post/"+post.ID, fields, hx), status)
			current, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			check(current.Markdown, []string{"photo-album", "file-downloads"})
			// A loaded historical editor uses the historical order and a baseline
			// containing the current order. Explicit restoration restores the order.
			selected, err := h.blog.history.GetRevision(t.Context(), "post", post.ID, original.ID)
			if err != nil {
				t.Fatal(err)
			}
			check(selected.Markdown, []string{"file-downloads", "photo-album"})
			if err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), "post", post.ID, current.Version, selected); err != nil {
				t.Fatal(err)
			}
			restored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			check(restored.Markdown, []string{"file-downloads", "photo-album"})
			usage, err := h.blog.mediaReferences.ReferencedMedia(t.Context(), file.ID, photo.ID)
			if err != nil || !usage[file.ID].Current || !usage[photo.ID].Current {
				t.Fatal("ordering lost media references", usage, err)
			}
		})
	}
}

func TestComponentOrderCompatibility(t *testing.T) {
	photo := albumPhoto{ID: strings.Repeat("a", 64) + ".jpeg", Caption: "A saved photo"}
	file := postDownload{ID: strings.Repeat("b", 64) + ".txt", Label: "A saved file", Size: 17}
	legacy, err := json.Marshal(map[string]any{"version": 1, "album": []albumPhoto{photo}, "downloads": []postDownload{file}})
	if err != nil {
		t.Fatal(err)
	}
	oldSource := "Body\n\n```post-components\n" + string(legacy) + "\n```\n"
	page := editorPage{Post: Post{Markdown: oldSource}}
	if page.ComponentOrderJSON() != `["photo-album","file-downloads"]` {
		t.Fatal("legacy order changed", page.ComponentOrderJSON())
	}
	for _, order := range []string{`["file-downloads","photo-album"]`, `["photo-album","file-downloads"]`, ""} {
		source, err := joinPostComponents(page.BodyMarkdown(), page.AlbumMarkdown(), page.DownloadsJSON(), order)
		if err != nil {
			t.Fatal(err)
		}
		_, components, err := splitPostComponents(source)
		if err != nil || components.Version != 2 || !slices.Equal(components.Album(), []albumPhoto{photo}) || !slices.Equal(components.Downloads(), []postDownload{file}) {
			t.Fatal("component upgrade lost content", components, err)
		}
		rendered, err := markdown(source)
		if err != nil {
			t.Fatal(err)
		}
		files := strings.Index(string(rendered), `class="file-downloads"`)
		album := strings.Index(string(rendered), `class="photo-album"`)
		if files < 0 || album < 0 || (files < album) != (order == `["file-downloads","photo-album"]`) {
			t.Fatal("rendered order differs", order, rendered)
		}
		search := searchableWriting(source)
		if (strings.Index(search, file.Label) < strings.Index(search, photo.Caption)) != (files < album) {
			t.Fatal("search text order differs from reader order")
		}
		reopened := editorPage{Post: Post{Markdown: source}}
		if !slices.Equal(reopened.EditorComponents(), components.Order()) {
			t.Fatal("editor order differs from reader")
		}
	}
	for _, order := range []string{`["unknown"]`, `["photo-album","photo-album"]`, `{"type":"photo-album"}`, `[1]`, `[] trailing`} {
		if _, err := joinPostComponents("Body", page.AlbumMarkdown(), page.DownloadsJSON(), order); err == nil {
			t.Fatal("invalid order accepted", order)
		}
	}
}

func TestUnknownOrderedComponentsPreserved(t *testing.T) {
	for _, envelope := range []string{
		`{"version":3,"components":[]}`,
		`{"version":2,"components":[{"type":"video","url":"keep this source"}]}`,
		`{"version":2,"components":[{"type":"photo-album","photos":[{"id":"invalid"}]}]}`,
		`{"version":2,"components":[{"type":"file-downloads","files":[],"extra":"keep"}]}`,
	} {
		source := "```post-components\n" + envelope + "\n```\n"
		page := editorPage{Post: Post{Markdown: source}}
		if page.BodyMarkdown() != source || page.AlbumMarkdown() != "" || page.DownloadsJSON() != "" {
			t.Fatal("unsupported component source was discarded")
		}
		if _, err := joinPostComponents(source, "", "", ""); err == nil {
			t.Fatal("unsupported component saved silently")
		}
	}
}

func TestPostComponentsMalformedAndNested(t *testing.T) {
	entry := postDownload{ID: strings.Repeat("a", 64) + ".txt", Label: "Download", Size: 10}
	valid, err := joinPostComponents("Body", "", downloadSource(t, entry), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		"```post-components\n{\"version\":2,\"unknown\":\"must stay\"}\n```\n",
		"```post-components\nnot JSON\n```\n",
		"```post-components\n{\"version\":1,\"extra\":true}\n```\n",
		"```photo-album\nmalformed legacy photo source\n```\n",
	} {
		page := editorPage{Post: Post{Markdown: source}}
		if page.BodyMarkdown() != source || page.DownloadsJSON() != "" || page.AlbumMarkdown() != "" {
			t.Fatal("unsupported data discarded")
		}
		if _, err := joinPostComponents(source, "", "", ""); err == nil {
			t.Fatal("unsupported components saved silently")
		}
		rendered, err := markdown(source)
		if err != nil || !strings.Contains(string(rendered), "<pre><code") {
			t.Fatal("unsafe reader fallback", rendered, err)
		}
	}
	for _, source := range []string{
		"````markdown\n" + valid + "````\n",
		"> " + strings.ReplaceAll(strings.TrimSpace(valid), "\n", "\n> ") + "\n",
		valid + "\nWriting after a component example",
	} {
		body, components, err := splitPostComponents(source)
		if err != nil || body != source || len(components.Downloads()) != 0 {
			t.Fatal("ordinary example treated as component", err)
		}
		refs, err := contentMediaReferences(source, "/", "")
		if err != nil || len(refs) != 0 {
			t.Fatal("example indexed as attachment", refs, err)
		}
	}
	for _, body := range []string{"```go\nunfinished", "<script>\nunclosed"} {
		if _, err := joinPostComponents(body, "", downloadSource(t, entry), ""); err == nil {
			t.Fatal("component swallowed by body")
		}
	}
}
