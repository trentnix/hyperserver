package blog

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAdditionalSocialPostURLs(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{"http://old.reddit.com/r/golang/comments/abc123/a_post/def456/?tracking=1", "https://www.reddit.com/r/golang/comments/abc123/a_post/def456/"},
		{"https://www.reddit.com/r/reddit/comments/135shj5/こんにちは/", "https://www.reddit.com/r/reddit/comments/135shj5/%E3%81%93%E3%82%93%E3%81%AB%E3%81%A1%E3%81%AF/"},
		{"https://reddit.com/r/reddit/comments/135shj5/%E3%81%93%E3%82%93%E3%81%AB%E3%81%A1%E3%81%AF/abc123/?tracking=1", "https://www.reddit.com/r/reddit/comments/135shj5/%E3%81%93%E3%82%93%E3%81%AB%E3%81%A1%E3%81%AF/abc123/"},
		{"https://reddit.com/r/reddit/comments/135shj5/café_世界/", "https://www.reddit.com/r/reddit/comments/135shj5/cafe%CC%81_%E4%B8%96%E7%95%8C/"},
		{"https://instagram.com/reel/Ab_cd-123/?igsh=123", "https://www.instagram.com/reel/Ab_cd-123/"},
		{"https://linkedin.com/posts/a-person_a-post-activity-123456789-abCD/?tracking=1", "https://www.linkedin.com/feed/update/urn:li:activity:123456789"},
		{"https://www.linkedin.com/feed/update/urn:li:ugcPost:123456789/", "https://www.linkedin.com/feed/update/urn:li:ugcPost:123456789"},
		{"https://m.tiktok.com/@user.name/photo/123456789?lang=en", "https://www.tiktok.com/@user.name/photo/123456789"},
		{"https://MASTODON.SOCIAL/users/Mastodon/statuses/123456789?tracking=1", "https://mastodon.social/@Mastodon/123456789"},
	} {
		provider, err := socialPostProvider(test.raw)
		if err != nil {
			t.Fatal(test.raw, err)
		}
		got, err := provider.postURL(test.raw)
		if err != nil || got != test.want {
			t.Fatalf("%s: got %q, %v, want %q", test.raw, got, err, test.want)
		}
	}
	for _, raw := range []string{
		"https://reddit.com/r/golang", "https://reddit.com/r/golang/s/abc", "https://reddit.com/login",
		"https://reddit.com/r/golang/comments/abc123/title%2Fdef456", "https://reddit.com/r%2Fgolang/comments/abc123/title",
		"https://reddit.com/r/golang/comments/abc123/..", "https://reddit.com/r/golang/comments/abc123/%FF",
		"https://instagram.com/username", "https://instagram.com/stories/user/123", "https://instagram.com/p/abcdef/embed/",
		"https://linkedin.com/in/person", "https://linkedin.com/company/example", "https://linkedin.com/feed/update/urn:li:activity:bad",
		"https://tiktok.com/@user", "https://vm.tiktok.com/abc123", "https://tiktok.com/player/v1/123",
		"https://127.0.0.1/@user/123", "https://[::1]/@user/123", "https://localhost/@user/123", "https://example.local/@user/123",
		"https://127.1/@user/123", "https://0x7f.0.0.1/@user/123",
		"https://evil.test/@user/123", "https://instance.invalid/@user/123", "http://mastodon.social/@user/123",
		"https://mastodon.social/@user/123/embed", "https://mastodon.social/@user%2F123", "https://mastodon.social:8443/@user/123",
	} {
		if _, err := socialPostProvider(raw); err == nil {
			t.Fatal("unsupported or unsafe URL accepted", raw)
		}
	}
	for _, test := range additionalSocialPostCases() {
		for _, raw := range []string{"https://user@" + strings.TrimPrefix(test.url, "https://"), test.url + "%2Fscript"} {
			if _, err := socialPostProvider(raw); err == nil {
				t.Fatal("unsafe URL accepted", raw)
			}
		}
		value := postSocialPost{URL: test.url, Description: "Context <script> & text", ImageID: strings.Repeat("b", 64) + ".png"}
		source, err := joinPostComponents("Body", componentInput{"socialPost": componentJSON(value), "componentOrder": `["social-post"]`})
		if err != nil {
			t.Fatal(err)
		}
		feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example"})
		if err != nil || !strings.Contains(string(feed), html.EscapeString(test.url)) || !strings.Contains(string(feed), "Context &lt;script&gt; &amp; text") || !strings.Contains(string(feed), "https://blog.example/images/") || strings.Contains(string(feed), "<iframe") || strings.Contains(string(feed), "<button") {
			t.Fatal("static output lost context or exposed reader controls", feed, err)
		}
	}
}

func TestRedditUnicodeRoundTrip(t *testing.T) {
	value := postSocialPost{URL: "https://www.reddit.com/r/reddit/comments/135shj5/こんにちは/", Description: "Unicode title"}
	source, err := joinPostComponents("Body", componentInput{"socialPost": componentJSON(value), "componentOrder": `["social-post"]`})
	if err != nil {
		t.Fatal(err)
	}
	_, components, err := splitPostComponents(source)
	if err != nil || components.Components[0].Content.EditorSource() != componentJSON(value) {
		t.Fatal("the authored URL must remain unchanged", components, err)
	}
	want := "https://www.reddit.com/r/reddit/comments/135shj5/%E3%81%93%E3%82%93%E3%81%AB%E3%81%A1%E3%81%AF/"
	reader, err := markdown(source)
	if err != nil || !strings.Contains(string(reader), `data-social-post="`+want+`"`) || !strings.Contains(string(reader), `href="`+want+`"`) {
		t.Fatal("the reader must use an escaped post URL", reader, err)
	}
	feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example"})
	if err != nil || !strings.Contains(string(feed), `href="`+want+`"`) || strings.Contains(string(feed), "data-social-post=") {
		t.Fatal("the feed must retain an ordinary escaped viewing link", feed, err)
	}
}

func TestMastodonEmbedBoundary(t *testing.T) {
	h := registeredBrowserHarness(t)
	callback := strings.Repeat("a", 32)
	path := "/social/mastodon?" + url.Values{"url": {"https://MASTODON.SOCIAL/@Mastodon/123?tracking=1"}, "id": {callback}}.Encode()
	response := h.request("GET", path, nil, false)
	requireStatus(t, response, http.StatusOK)
	if response.Header().Get("X-Frame-Options") != "SAMEORIGIN" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("the wrapper must be frameable only by the same site and must not be cached", response.Header())
	}
	policy := response.Header().Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self'", "frame-src https://mastodon.social;", "frame-ancestors 'self'"} {
		if !strings.Contains(policy, want) {
			t.Fatal("missing wrapper restriction", policy)
		}
	}
	for _, want := range []string{`src="https://mastodon.social/@Mastodon/123/embed"`, `data-origin="https://mastodon.social"`, `data-callback="` + callback + `"`, `allow="autoplay 'none'; fullscreen; picture-in-picture"`, "mastodon-embed.js"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatal("missing escaped wrapper output", want, response.Body.String())
		}
	}
	for _, raw := range []string{"https://localhost/@user/123", "https://192.168.1.1/@user/123", "https://evil.test/@user/123", "https://mastodon.social/@user/123%22%3E%3Cscript%3E", "https://mastodon.social/@user/123/embed"} {
		bad := "/social/mastodon?" + url.Values{"url": {raw}, "id": {callback}}.Encode()
		requireStatus(t, h.request("GET", bad, nil, false), http.StatusBadRequest)
	}
	for _, suffix := range []string{"&id=" + callback, "&url=https://other.social/@user/123", "&id=invalid"} {
		requireStatus(t, h.request("GET", path+suffix, nil, false), http.StatusBadRequest)
	}
	requireStatus(t, h.request("GET", "/social/mastodon?url=https://mastodon.social/@user/123", nil, false), http.StatusBadRequest)
	requireStatus(t, h.request("POST", path, nil, false), http.StatusNotFound)
	if strings.Contains(ContentSecurityPolicy, "frame-src https:") || strings.Contains(ContentSecurityPolicy, "frame-src *") {
		t.Fatal("Mastodon must not broaden the blog document's frame policy")
	}
}
