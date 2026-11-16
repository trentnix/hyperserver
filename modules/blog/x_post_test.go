package blog

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestXPostURLs(t *testing.T) {
	for _, raw := range []string{
		"https://x.com/TwitterDev/status/560070183650213889",
		"https://www.twitter.com/user/status/560070183650213889?s=20&t=tracking",
		"http://mobile.twitter.com/user/status/560070183650213889/",
		"https://x.com/i/web/status/560070183650213889",
		"https://x.com/i/status/560070183650213889",
		"https://x.com/user/status/560070183650213889/photo/1",
		"https://x.com/user/status/560070183650213889/video/1",
	} {
		if id, err := xPostURL(raw); err != nil || id != "560070183650213889" {
			t.Errorf("share URL %q: id=%q err=%v", raw, id, err)
		}
	}
	for _, raw := range []string{
		"javascript:alert(1)", "https://x.com.evil.test/user/status/123", "https://evil.test/x.com/user/status/123",
		"https://user@x.com/user/status/123", "https://x.com:8443/user/status/123", "https://x.com/user",
		"https://x.com/user/status/0", "https://x.com/user/status/01", "https://x.com/user/status/-1",
		"https://x.com/user/status/123456789012345678901", "https://x.com/user/status/1e9",
		"https://x.com/user/status/%31", "https://x.com/user/status/123/extra", "https://x.com/user/status/123/photo/0",
		"https://x.com/user/status/123/video/99", "https://x.com//user/status/123", "https://x.com/user/status/123//",
		"https://x.com/username_is_too_long/status/123", "https://x.com/user/status/123/<script>",
		"https://x.com/user/status/123\\@evil.test", "https://x.com/intent/post?text=hello",
	} {
		if _, err := xPostURL(raw); err == nil {
			t.Errorf("accepted invalid post URL %q", raw)
		}
	}
}

func TestXPostComponent(t *testing.T) {
	post := postXPost{URL: "https://twitter.com/user/status/560070183650213889?s=20", Description: "A <script>description</script> & context.\nMore context.", ImageID: strings.Repeat("b", 64) + ".png"}
	source, err := joinPostComponents("Body", componentInput{"xPost": componentJSON(post), "video": `{"url":"https://youtu.be/M7lc1UVf-VE"}`, "audio": componentJSON(postAudio{ID: strings.Repeat("a", 64) + ".mp3", Size: 1, Title: "A saved recording"}), "componentOrder": `["x-post","video","audio"]`})
	if err != nil {
		t.Fatal(err)
	}
	body, components, err := splitPostComponents(source)
	if err != nil || body != "Body" || len(components.Components) != 3 || components.Components[0].Type != "x-post" || components.Components[0].Content.EditorSource() != componentJSON(post) {
		t.Fatal("X post did not round trip in its chosen order", components, err)
	}
	rendered, err := markdown(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{html.EscapeString(post.Description), `data-x-post="560070183650213889"`, `href="https://x.com/i/web/status/560070183650213889"`, `hx-boost="false"`, `data-x-post-fallback`, `src="/images/` + post.ImageID + `?variant=display"`} {
		if !strings.Contains(string(rendered), want) {
			t.Fatalf("missing %q in %s", want, rendered)
		}
	}
	if strings.Contains(string(rendered), "<script>") || strings.Contains(string(rendered), "platform.twitter.com") || strings.Index(string(rendered), "x-post-component") > strings.Index(string(rendered), "video-component") {
		t.Fatal("unsafe rendering or incorrect order", rendered)
	}
	feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example"})
	if err != nil || !strings.Contains(string(feed), "Open on X") || !strings.Contains(string(feed), html.EscapeString(post.Description)) || !strings.Contains(string(feed), "Open on YouTube") || !strings.Contains(string(feed), "https://blog.example/images/"+post.ImageID) {
		t.Fatal("feed lost fallback content or the following component", feed, err)
	}
	for _, interactive := range []string{"Load X post", "Loading connects", "data-reader-only", "<button", "<iframe", "<audio", "</audio>"} {
		if strings.Contains(string(feed), interactive) {
			t.Fatal("reader controls leaked into the feed", interactive, feed)
		}
	}
	search := searchableWriting(source)
	if !strings.Contains(search, post.Description) || strings.Contains(search, "560070183650213889") || strings.Contains(search, "xPost") {
		t.Fatal("search must use authored text", search)
	}
	refs, err := contentMediaReferences(source, "/post/s/x-post", "")
	if err != nil || len(refs) != 2 || !slices.Contains(refs, "/files/"+strings.Repeat("a", 64)+".mp3") || !slices.Contains(refs, "/images/"+post.ImageID) {
		t.Fatal("mixed components must retain local audio and fallback image references", refs, err)
	}
	for _, raw := range []string{`null`, `{}`, `{"url":""}`, `{"url":"https://x.com/user/status/123","html":"bad"}`, `{"url":"https://x.com/user/status/123"} {}`, componentJSON(postXPost{URL: post.URL, Description: strings.Repeat("a", 1001)}), componentJSON(postXPost{URL: post.URL, ImageID: "https://example.test/screenshot.png"}), componentJSON(postXPost{ImageID: post.ImageID})} {
		if _, err := joinPostComponents("Body", componentInput{"xPost": raw}); err == nil {
			t.Fatal("invalid component accepted", raw)
		}
	}
	withoutImage, err := parseXPost(`{"url":"https://x.com/user/status/123"}`)
	if err != nil || len(withoutImage.Media()) != 0 {
		t.Fatal("existing X posts must work without a fallback image", withoutImage, err)
	}
}

func TestXPostFormsAndHistory(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := registeredBrowserHarness(t)
			h.login(t)
			image := mediaUpload(t, h.blog, "X post screenshot").ID
			value := postXPost{URL: "https://x.com/user/status/560070183650213889", Description: "Keep this context.", ImageID: image}
			input := componentJSON(value)
			fields := url.Values{"title": {""}, "slug": {"x-post-review"}, "xPost": {input}, "componentOrder": {`["x-post"]`}}
			response := h.request("POST", "/post", fields, hx)
			requireStatus(t, response, http.StatusUnprocessableEntity)
			if !hx && !strings.Contains(html.UnescapeString(response.Body.String()), input) {
				t.Fatal("rejected save lost X post data")
			}
			fields.Set("title", "X post review")
			missing := value
			missing.ImageID = strings.Repeat("c", 64) + ".png"
			fields.Set("xPost", componentJSON(missing))
			requireStatus(t, h.request("POST", "/post", fields, hx), http.StatusUnprocessableEntity)
			fields.Set("xPost", input)
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, h.request("POST", "/post", fields, hx), status)
			post, err := h.blog.posts.GetBySlug(t.Context(), "x-post-review", true)
			if err != nil {
				t.Fatal(err)
			}
			public := func() int {
				response := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(response, httptest.NewRequest("GET", "https://blog.example/images/"+image+"?variant=display", nil))
				return response.Code
			}
			if public() != http.StatusNotFound {
				t.Fatal("a draft screenshot must remain private")
			}
			versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
			if err != nil || len(versions) != 1 {
				t.Fatal("missing revision", versions, err)
			}
			original, err := h.blog.history.GetRevision(t.Context(), "post", post.ID, versions[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			post, err = setPostPublication(h.blog.posts, t.Context(), post.ID, true)
			if err != nil || public() != http.StatusOK {
				t.Fatal("a published screenshot must be available", err)
			}
			post.Markdown, post.Draft = "Component removed", true
			post, err = savePostFixture(h.blog.posts, t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			usage, err := h.blog.mediaReferences.ReferencedMedia(t.Context(), image)
			if err != nil || public() != http.StatusNotFound || usage[image].Current || len(usage[image].History) == 0 {
				t.Fatal("a removed screenshot must remain in history without public access", usage, err)
			}
			if err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), "post", post.ID, post.Version, original); err != nil {
				t.Fatal(err)
			}
			restored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil || restored.Markdown != original.Markdown {
				t.Fatal("revision restoration lost the X post", restored, err)
			}
			if _, err := setPostPublication(h.blog.posts, t.Context(), post.ID, true); err != nil {
				t.Fatal(err)
			}
			reader := requestArticle(h, "/post/s/x-post-review")
			requireStatus(t, reader, http.StatusOK)
			if !strings.Contains(reader.Body.String(), "x-post-component") || !strings.Contains(reader.Header().Get("Content-Security-Policy"), "script-src 'self';") {
				t.Fatal("reader must use the ordinary script policy", reader.Header())
			}
		})
	}
}

func TestXPostPermissions(t *testing.T) {
	h := usersHarness(t)
	admin, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	other, err := h.blog.persistPost(t.Context(), Post{Title: "Admin draft", Slug: "admin-draft", Markdown: "Private", Draft: true, AuthorID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	poster := registeredPoster(t, h)
	fields := url.Values{"title": {"Poster X post"}, "xPost": {`{"url":"https://x.com/user/status/123"}`}, "componentOrder": {`["x-post"]`}}
	image := mediaUpload(t, h.blog, "Private X screenshot").ID
	if err := h.blog.mediaMetadata.RememberMedia(t.Context(), image, mediaMetadata{Filename: "screenshot.png", UploadedBy: admin.ID}); err != nil {
		t.Fatal(err)
	}
	value := postXPost{URL: "https://x.com/user/status/123", ImageID: image}
	fields.Set("xPost", componentJSON(value))
	for _, hx := range []bool{false, true} {
		requireStatus(t, h.request("POST", "/post", fields, hx), http.StatusUnprocessableEntity)
	}
	value.ImageID = mediaUpload(t, h.blog, "Poster X screenshot").ID
	if err := h.blog.mediaMetadata.RememberMedia(t.Context(), value.ImageID, mediaMetadata{Filename: "own-screenshot.png", UploadedBy: poster.ID}); err != nil {
		t.Fatal(err)
	}
	fields.Set("xPost", componentJSON(value))
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusSeeOther)
	post, err := h.blog.posts.GetBySlug(t.Context(), "poster-x-post", true)
	if err != nil || post.AuthorID != poster.ID {
		t.Fatal("poster could not create their own X post component", post, err)
	}
	fields.Set("version", "1")
	for _, hx := range []bool{false, true} {
		requireStatus(t, h.request("POST", "/post/"+other.ID, fields, hx), http.StatusForbidden)
	}
}
