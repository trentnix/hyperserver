package blog

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func videoJSON(t *testing.T, video postVideo) string {
	t.Helper()
	data, err := json.Marshal(video)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestVideoURLs(t *testing.T) {
	for _, tc := range []struct {
		url, provider, id, hash string
		start                   int
	}{
		{"https://www.youtube.com/watch?v=M7lc1UVf-VE", "YouTube", "M7lc1UVf-VE", "", 0},
		{"https://youtu.be/M7lc1UVf-VE?si=tracking&t=1m30s", "YouTube", "M7lc1UVf-VE", "", 90},
		{"HTTP://m.youtube.com/shorts/M7lc1UVf-VE", "YouTube", "M7lc1UVf-VE", "", 0},
		{"https://youtube.com/live/M7lc1UVf-VE", "YouTube", "M7lc1UVf-VE", "", 0},
		{"https://www.youtube-nocookie.com/embed/M7lc1UVf-VE?start=90&autoplay=0", "YouTube", "M7lc1UVf-VE", "", 90},
		{"https://vimeo.com/76979871", "Vimeo", "76979871", "", 0},
		{"https://www.vimeo.com/123456789/5e2d1c1e6d#t=1m30s", "Vimeo", "123456789", "5e2d1c1e6d", 90},
		{"https://player.vimeo.com/video/123456789?h=5e2d1c1e6d#t=90s", "Vimeo", "123456789", "5e2d1c1e6d", 90},
	} {
		t.Run(tc.url, func(t *testing.T) {
			source, err := videoURL(tc.url)
			if err != nil || source.Provider != tc.provider || source.ID != tc.id || source.Hash != tc.hash || source.Start != tc.start {
				t.Fatal("incorrect provider or video details", source, err)
			}
			for _, normalized := range []string{source.viewingURL(), source.embedURL()} {
				roundTrip, err := videoURL(normalized)
				if err != nil || roundTrip != source {
					t.Fatal("canonical URLs must preserve the video and unlisted hash", normalized, roundTrip, err)
				}
			}
			if strings.Contains(source.embedURL(), "tracking") || strings.Contains(source.viewingURL(), "tracking") {
				t.Fatal("tracking parameters reached the player")
			}
		})
	}
	for _, raw := range []string{
		"javascript:alert(1)", "https://youtube.com.attacker.test/watch?v=M7lc1UVf-VE", "https://attacker.test/youtube.com/M7lc1UVf-VE",
		"https://user@youtube.com/watch?v=M7lc1UVf-VE", "https://youtube.com:8443/watch?v=M7lc1UVf-VE", "https://youtube.com/playlist?list=bad",
		"https://youtube.com/watch?v=short", "https://youtube.com/watch?v=M7lc1UVf-VE&v=differentID", "https://youtu.be/M7lc1UVf-VE/extra",
		"https://youtube.com/embed/%4D7lc1UVf-VE", "https://youtu.be/M7lc1UVf-VE?t=-1", "https://youtu.be/M7lc1UVf-VE?t=0.5s",
		"https://youtu.be/M7lc1UVf-VE?t=9999999999999999999999", "https://youtu.be/M7lc1UVf-VE?t=1000h", "https://vimeo.com/not-a-video",
		"https://vimeo.com/0", "https://vimeo.com/123/invalid-hash", "https://player.vimeo.com/video/123?h=one&h=two",
		"https://vimeo.com/123/one?h=two", "https://player.vimeo.com/123", "https://vimeo.com/123/one/extra",
		"https://x.com/user/status/123", "https://www.youtube.com\\@attacker.test/watch?v=M7lc1UVf-VE",
	} {
		if _, err := videoURL(raw); err == nil {
			t.Errorf("accepted unsupported video URL %q", raw)
		}
	}
}

func TestVideoComponent(t *testing.T) {
	video := postVideo{URL: "https://vimeo.com/76979871#t=1m30s", Title: `A <script>title</script>`, Caption: "A caption with <markup> & quotes.\nAnother line."}
	link := postLink{URL: "https://example.test/article", Title: "Related link"}
	source, err := joinPostComponents("Body", componentInput{"video": videoJSON(t, video), "link": linkSource(t, link), "componentOrder": `["video","link-preview"]`})
	if err != nil {
		t.Fatal(err)
	}
	body, components, err := splitPostComponents(source)
	if err != nil || body != "Body" || components.Video() == nil || *components.Video() != video || components.Order()[0] != "video" {
		t.Fatal("video did not round trip", components, err)
	}
	if got := (editorPage{Post: Post{Markdown: source}}).VideoJSON(); got != videoJSON(t, video) {
		t.Fatal("editor lost saved video details", got)
	}
	rendered, err := markdown(source)
	if err != nil || strings.Contains(string(rendered), "<script>") || !strings.Contains(string(rendered), "<iframe") || !strings.Contains(string(rendered), html.EscapeString(video.Title)) || !strings.Contains(string(rendered), html.EscapeString(video.Caption)) || strings.Index(string(rendered), `class="video-component"`) > strings.Index(string(rendered), `class="link-preview"`) {
		t.Fatal("unsafe or incorrect video rendering", rendered, err)
	}
	if !strings.Contains(string(rendered), `src="https://player.vimeo.com/video/76979871?autoplay=0&amp;dnt=1#t=90s"`) || strings.Contains(string(rendered), `href="https://vimeo.com/`) {
		t.Fatal("the player must use a normalized URL without a redundant viewing link", rendered)
	}
	for _, want := range []string{`loading="lazy"`, `referrerpolicy="strict-origin"`, `allow="encrypted-media; fullscreen; picture-in-picture"`} {
		if !strings.Contains(string(rendered), want) {
			t.Fatal("player preview is missing its loading, referrer, or fullscreen settings", want)
		}
	}
	if strings.Contains(string(rendered), "allowfullscreen") {
		t.Fatal("fullscreen permission must not be declared twice", rendered)
	}
	if strings.Contains(string(rendered), "autoplay=1") || strings.Contains(string(rendered), "<button") {
		t.Fatal("the provider preview must appear without an extra play button or autoplay", rendered)
	}
	refs, err := contentMediaReferences(source, "/post/s/video", "")
	if err != nil || len(refs) != 0 {
		t.Fatal("remote videos must not invent local file references", refs, err)
	}
	feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example", Path: "/post/s/video"})
	if err != nil || !strings.Contains(string(feed), "Open on Vimeo") || !strings.Contains(string(feed), "Another line.") || strings.Contains(string(feed), "<iframe") {
		t.Fatal("feeds need a useful title, link, and caption without a player", feed, err)
	}
	search := searchableWriting(source)
	if !strings.Contains(search, video.Title) || !strings.Contains(search, video.Caption) || strings.Contains(search, "76979871") || strings.Contains(search, `"video"`) {
		t.Fatal("search must index video text instead of serialized data", search)
	}
	for _, raw := range []string{`null`, `{"url":"","title":"Video"}`, `{"url":"https://youtu.be/M7lc1UVf-VE","title":"Video","iframe":"bad"}`, `{"url":"https://youtu.be/M7lc1UVf-VE","title":"Video"} {}`} {
		if _, err := joinPostComponents("Body", componentInput{"video": raw}); err == nil {
			t.Fatal("invalid video accepted", raw)
		}
	}
	for _, raw := range []string{
		`{"version":2,"components":[{"type":"video"}]}`,
		`{"version":2,"components":[{"type":"link-preview","link":{"url":"https://example.test/","title":"Link"},"video":{"url":"https://youtu.be/M7lc1UVf-VE","title":"Video"}}]}`,
		`{"version":2,"components":[{"type":"video","video":{"url":"https://youtu.be/M7lc1UVf-VE","title":"Video"}},{"type":"video","video":{"url":"https://youtu.be/M7lc1UVf-VE","title":"Video"}}]}`,
	} {
		if _, err := parseComponents(raw); err == nil {
			t.Fatal("mixed or repeated components accepted", raw)
		}
	}
}

func TestVideoOptionalTitle(t *testing.T) {
	for _, video := range []postVideo{
		{URL: "https://youtu.be/M7lc1UVf-VE"},
		{URL: "https://vimeo.com/76979871", Title: " \t\n"},
	} {
		source, err := joinPostComponents("", componentInput{"video": videoJSON(t, video)})
		if err != nil {
			t.Fatal("a video URL must be sufficient without a title or caption", err)
		}
		_, components, err := splitPostComponents(source)
		if err != nil || components.Video() == nil || *components.Video() != video {
			t.Fatal("an untitled video must round trip without adding a stored title", components, err)
		}
		provider, _ := videoURL(video.URL)
		rendered, err := markdown(source)
		if err != nil || strings.Contains(string(rendered), "<h3") || !strings.Contains(string(rendered), `title="`+provider.Provider+` video"`) || strings.Contains(string(rendered), "<a ") {
			t.Fatal("an untitled player needs an accessibility label without an empty heading", rendered, err)
		}
		feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example"})
		if err != nil || !strings.Contains(string(feed), "Open on "+provider.Provider) || strings.Contains(string(feed), "<iframe") {
			t.Fatal("an untitled video needs a usable feed link", feed, err)
		}
	}
	for _, title := range []string{strings.Repeat("a", 241), string([]byte{0xff})} {
		if err := validateVideo(&postVideo{URL: "https://youtu.be/M7lc1UVf-VE", Title: title}); err == nil {
			t.Fatal("optional titles must still satisfy length and encoding limits")
		}
	}
}

func TestVideoFormsAndHistory(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := newHarness(t)
			h.register(t)
			h.verify(t)
			h.login(t)
			video := postVideo{URL: "https://youtu.be/M7lc1UVf-VE", Caption: "A saved caption"}
			fields := url.Values{"title": {""}, "slug": {"video-review"}, "video": {videoJSON(t, video)}, "componentOrder": {`["video"]`}}
			response := h.request("POST", "/post", fields, hx)
			requireStatus(t, response, http.StatusUnprocessableEntity)
			if !hx && !strings.Contains(html.UnescapeString(response.Body.String()), videoJSON(t, video)) {
				t.Fatal("validation discarded submitted video details")
			}
			fields.Set("title", "Video review")
			status := http.StatusSeeOther
			if hx {
				status = http.StatusOK
			}
			requireStatus(t, h.request("POST", "/post", fields, hx), status)
			post, err := h.blog.posts.GetBySlug(t.Context(), "video-review", true)
			if err != nil {
				t.Fatal(err)
			}
			versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
			if err != nil || len(versions) != 1 {
				t.Fatal("video revision was not saved", versions, err)
			}
			original, err := h.blog.history.GetRevision(t.Context(), "post", post.ID, versions[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			post.Markdown = "Video removed"
			post, err = savePostFixture(h.blog.posts, t.Context(), post)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), "post", post.ID, post.Version, original); err != nil {
				t.Fatal(err)
			}
			restored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil || restored.Markdown != original.Markdown {
				t.Fatal("history restoration lost the video", restored, err)
			}
		})
	}
}

func TestVideoPermissions(t *testing.T) {
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
	fields := url.Values{"title": {"Poster video"}, "video": {videoJSON(t, postVideo{URL: "https://vimeo.com/76979871", Title: "Video"})}}
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusSeeOther)
	post, err := h.blog.posts.GetBySlug(t.Context(), "poster-video", true)
	if err != nil || post.AuthorID != poster.ID {
		t.Fatal("poster could not create their video post", post, err)
	}
	// Video components use the same post authorization boundary as other content.
	fields.Set("version", "1")
	requireStatus(t, h.request("POST", "/post/"+other.ID, fields, false), http.StatusForbidden)
}
