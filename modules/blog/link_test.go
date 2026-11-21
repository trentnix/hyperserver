package blog

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func linkSource(t *testing.T, link postLink) string {
	t.Helper()
	data, err := json.Marshal(link)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLinkComponent(t *testing.T) {
	link := postLink{URL: "https://example.test/story?query=a&other=b", Title: `A <script>title</script>`, Description: `A "description"`, SiteName: "Example & site", ImageID: strings.Repeat("a", 64) + ".png"}
	for _, hidden := range []bool{false, true} {
		link.HideImage = hidden
		source, err := joinPostComponents("Body", componentInput{"link": linkSource(t, link), "componentOrder": `["link-preview"]`})
		if err != nil {
			t.Fatal(err)
		}
		body, components, err := splitPostComponents(source)
		if err != nil || body != "Body" || components.Link() == nil || *components.Link() != link {
			t.Fatal("link did not round trip", components, err)
		}
		page := newComponentEditorForm(source, nil)
		if page.fields["link"] != linkSource(t, link) {
			t.Fatal("editor discarded the link")
		}
		rendered, err := markdown(source)
		if err != nil || strings.Contains(string(rendered), "<script>") || !strings.Contains(string(rendered), html.EscapeString(link.Title)) || !strings.Contains(string(rendered), `href="https://example.test/story?query=a&amp;other=b"`) || strings.Contains(string(rendered), "<img") == hidden {
			t.Fatal("unsafe or incorrect card", rendered, err)
		}
		refs, err := contentMediaReferences(source, "/post/s/link", "")
		if err != nil || !slices.Contains(refs, "/images/"+link.ImageID) {
			t.Fatal("hidden thumbnails must retain their saved reference", refs, err)
		}
		feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example", Path: "/post/s/link"})
		if err != nil || !strings.Contains(string(feed), "https://example.test/story") || !hidden && !strings.Contains(string(feed), "https://blog.example/images/"+link.ImageID) {
			t.Fatal("feed card needs absolute destinations", feed, err)
		}
		search := searchableWriting(source)
		if !strings.Contains(search, link.Title) || !strings.Contains(search, link.Description) || !strings.Contains(search, link.SiteName) || strings.Contains(search, link.ImageID) {
			t.Fatal("incorrect search text", search)
		}
	}
	for _, source := range []string{`null`, `{"url":"javascript:alert(1)","title":"Bad"}`, `{"url":"https://example.test/","title":""}`, `{"url":"https://example.test/","title":"Good","imageID":"https://attacker.test/track.png"}`, `{"url":"https://example.test/","title":"Good","unexpected":true}`} {
		if _, err := joinPostComponents("Body", componentInput{"link": source}); err == nil {
			t.Fatal("invalid link accepted", source)
		}
	}
	file := postDownload{ID: strings.Repeat("b", 64) + ".txt", Label: "Notes"}
	source, err := joinPostComponents("Body", componentInput{"downloads": downloadSource(t, file), "link": linkSource(t, link), "componentOrder": `["link-preview","file-downloads"]`})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := markdown(source)
	if err != nil || strings.Index(string(rendered), `class="link-preview"`) >= strings.Index(string(rendered), `class="file-downloads"`) {
		t.Fatal("link order was not preserved", rendered, err)
	}
}

func TestLinkFormsHistoryAndAccess(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			image := mediaUpload(t, h.blog, "Link thumbnail")
			link := postLink{URL: "https://example.test/article", Title: "Linked article", ImageID: image.ID, HideImage: true}
			fields := url.Values{"title": {"Link review"}, "slug": {"link-review"}, "link": {linkSource(t, link)}, "componentOrder": {`["link-preview"]`}}
			fields.Set("title", "")
			rejected := h.request("POST", "/post", fields, hx)
			requireStatus(t, rejected, http.StatusUnprocessableEntity)
			if !hx && !strings.Contains(html.UnescapeString(rejected.Body.String()), linkSource(t, link)) {
				t.Fatal("ordinary validation discarded the link")
			}
			fields.Set("title", "Link review")
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, h.request("POST", "/post", fields, hx), status)
			post, err := h.blog.posts.GetBySlug(t.Context(), "link-review", true)
			if err != nil {
				t.Fatal(err)
			}
			versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
			if err != nil || len(versions) != 1 {
				t.Fatal("missing link revision", err)
			}
			original, err := h.blog.history.GetRevision(t.Context(), "post", post.ID, versions[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			public := func() int {
				w := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(w, httptest.NewRequest("GET", "https://blog.example/images/"+image.ID, nil))
				return w.Code
			}
			if public() != http.StatusNotFound {
				t.Fatal("draft thumbnail became public")
			}
			post.Draft = false
			post.Published = time.Now().Add(time.Hour)
			post, err = savePostFixture(h.blog.posts, t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			if public() != http.StatusNotFound {
				t.Fatal("scheduled thumbnail became public")
			}
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			// Clear the future schedule for the publication check.
			post.Published = time.Now().Add(-time.Second)
			post, err = savePostFixture(h.blog.posts, t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			if public() != http.StatusOK {
				t.Fatal("saved thumbnail lost its publication reference")
			}
			usage, err := h.blog.mediaReferences.ReferencedMedia(t.Context(), image.ID)
			if err != nil || !usage[image.ID].Current {
				t.Fatal("hidden thumbnail was not retained", usage, err)
			}
			post.Draft = true
			post.Markdown = "Link removed"
			post, err = savePostFixture(h.blog.posts, t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			if public() != http.StatusNotFound {
				t.Fatal("historical thumbnail remained public")
			}
			if err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), "post", post.ID, post.Version, original); err != nil {
				t.Fatal(err)
			}
			restored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil || restored.Markdown != original.Markdown {
				t.Fatal("restoration discarded link settings", err)
			}
			link.ImageID = strings.Repeat("a", 64) + ".png"
			fields.Set("link", linkSource(t, link))
			requireStatus(t, h.request("POST", "/post", fields, hx), http.StatusUnprocessableEntity)
		})
	}
}
