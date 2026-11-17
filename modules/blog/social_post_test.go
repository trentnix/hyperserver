package blog

import (
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func socialPostCases() []struct {
	provider socialProvider
	url      string
} {
	return []struct {
		provider socialProvider
		url      string
	}{
		{blueskyProvider(), "https://bsky.app/profile/example.bsky.social/post/3kabc12345678"},
		{threadsProvider(), "https://www.threads.com/@example/post/C1234567890"},
		{facebookProvider(), "https://www.facebook.com/example/posts/123456789"},
	}
}

func additionalSocialPostCases() []struct {
	provider socialProvider
	url      string
} {
	return []struct {
		provider socialProvider
		url      string
	}{
		{socialProvider{Kind: "reddit-post", Name: "Reddit"}, "https://www.reddit.com/r/reddit/comments/135shj5/making_it_easier_to_share/"},
		{socialProvider{Kind: "mastodon-post", Name: "Mastodon"}, "https://mastodon.social/@Mastodon/117625790631441803"},
		{socialProvider{Kind: "instagram-post", Name: "Instagram"}, "https://www.instagram.com/p/fA9uwTtkSN/"},
		{socialProvider{Kind: "linkedin-post", Name: "LinkedIn"}, "https://www.linkedin.com/feed/update/urn:li:activity:7062468048728842240"},
		{socialProvider{Kind: "tiktok-post", Name: "TikTok"}, "https://www.tiktok.com/@scout2015/video/6718335390845095173"},
	}
}

func TestSocialPostURLs(t *testing.T) {
	for _, test := range []struct {
		provider  socialProvider
		raw, want string
	}{
		{blueskyProvider(), "https://bsky.app/profile/example.bsky.social/post/3kabc12345678?tracking=1#text", "https://bsky.app/profile/example.bsky.social/post/3kabc12345678"},
		{blueskyProvider(), "https://bsky.app/profile/did:plc:z72i7hdynmk6r22z27h6tvur/post/3kabc12345678/", "https://bsky.app/profile/did:plc:z72i7hdynmk6r22z27h6tvur/post/3kabc12345678"},
		{threadsProvider(), "http://threads.net/@user.name/post/Ab_cd-123/?x=1", "https://www.threads.com/@user.name/post/Ab_cd-123"},
		{threadsProvider(), "https://threads.com/t/Ab_cd-123?x=1", "https://www.threads.com/t/Ab_cd-123"},
		{facebookProvider(), "https://m.facebook.com/example/posts/pfbid0Abc123?mibextid=tracking", "https://www.facebook.com/example/posts/pfbid0Abc123"},
		{facebookProvider(), "https://www.facebook.com/story.php?story_fbid=123&id=456&tracking=1", "https://www.facebook.com/permalink.php?id=456&story_fbid=123"},
	} {
		got, err := test.provider.postURL(test.raw)
		if err != nil || got != test.want {
			t.Fatalf("%s: got %q, %v, want %q", test.raw, got, err, test.want)
		}
	}
	for _, test := range socialPostCases() {
		t.Run(test.provider.Name, func(t *testing.T) {
			for _, raw := range []string{"", "javascript:alert(1)", "https://example.test/post/123", strings.Replace(test.url, "https://", "https://user@", 1), strings.Replace(test.url, "/post", ":8443/post", 1), test.url + "/embed/", test.url + "%2Fscript", strings.Repeat("a", 2049)} {
				if _, err := test.provider.postURL(raw); err == nil {
					t.Fatal("invalid post URL accepted", raw)
				}
			}
		})
	}
	for _, raw := range []string{"https://www.facebook.com/plugins/post.php?href=https://evil.test", "https://facebook.com/share/p/123", "https://facebook.com/permalink.php?story_fbid=123&id=456&id=789", "https://facebook.com/example/posts/javascript:alert(1)", "https://facebook.com/profile.php?id=123"} {
		if _, err := facebookProvider().postURL(raw); err == nil {
			t.Fatal("unsupported Facebook URL accepted", raw)
		}
	}
}

func TestSocialPostComponents(t *testing.T) {
	input := componentInput{"xPost": `{"url":"https://x.com/user/status/123"}`}
	order := []string{"x-post"}
	for _, test := range socialPostCases() {
		value := postSocialPost{URL: test.url + "?tracking=1", Description: "A <script>post</script> & context.\nMore context.", ImageID: strings.Repeat("b", 64) + ".png"}
		input[test.provider.Field] = componentJSON(value)
		order = append(order, test.provider.Kind)
		for _, raw := range []string{"null", "{}", `{"url":""}`, `{"url":false}`, componentJSON(postSocialPost{URL: test.url, Description: strings.Repeat("界", 1001)}), componentJSON(postSocialPost{URL: test.url, ImageID: "https://evil.test/image.png"}), componentJSON(value) + " {}", strings.TrimSuffix(componentJSON(value), "}") + `,"html"
 "errors":"<script>"}`} {
			if _, err := parseSocialPost(raw); err == nil {
				t.Fatal("invalid component accepted", test.provider.Name, raw)
			}
		}
	}
	input["componentOrder"] = componentJSON(order)
	source, err := joinPostComponents("Body", input)
	if err != nil {
		t.Fatal(err)
	}
	body, components, err := splitPostComponents(source)
	if err != nil || body != "Body" || !slices.Equal(components.Order(), order) {
		t.Fatal("component order did not round trip", body, components, err)
	}
	reader, err := markdown(source)
	if err != nil {
		t.Fatal(err)
	}
	for i, test := range socialPostCases() {
		if components.Components[i+1].Content.EditorSource() != input[test.provider.Field] {
			t.Fatal("saved source changed", test.provider.Name)
		}
		for _, want := range []string{`data-social-provider="` + test.provider.Kind + `"`, `data-social-post="` + html.EscapeString(test.url) + `"`, "Open on " + test.provider.Name, "A &lt;script&gt;post&lt;/script&gt; &amp; context."} {
			if !strings.Contains(string(reader), want) {
				t.Fatal("missing safe reader output", want, reader)
			}
		}
	}
	feed, err := markdownAt(source, &url.URL{Scheme: "https", Host: "blog.example"})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"<iframe", "<button", "data-social-post=", "<script"} {
		if strings.Contains(string(feed), forbidden) {
			t.Fatal("interactive content leaked into a static view", forbidden, feed)
		}
	}
	if !strings.Contains(string(feed), `https://blog.example/images/`) || !strings.Contains(searchableWriting(source), "More context.") || strings.Contains(searchableWriting(source), "blueskyPost") {
		t.Fatal("static media or authored search text missing", feed, searchableWriting(source))
	}
	refs, err := contentMediaReferences(source, "/post/s/social", "")
	if err != nil || !slices.Contains(refs, "/images/"+strings.Repeat("b", 64)+".png") {
		t.Fatal("fallback image reference missing", refs, err)
	}
}

func TestSocialPostFormsAndHistory(t *testing.T) {
	for _, test := range append(socialPostCases(), additionalSocialPostCases()...) {
		for _, hx := range []bool{false, true} {
			t.Run(test.provider.Name+map[bool]string{false: "/ordinary", true: "/htmx"}[hx], func(t *testing.T) {
				h := registeredBrowserHarness(t)
				h.login(t)
				image := mediaUpload(t, h.blog, "Social screenshot").ID
				input := componentJSON(postSocialPost{URL: test.url, Description: "Keep this context.", ImageID: image})
				fields := url.Values{"title": {""}, "slug": {"social-review"}, "socialPost": {input}, "componentOrder": {componentJSON([]string{"social-post"})}}
				response := h.request("POST", "/post", fields, hx)
				requireStatus(t, response, http.StatusUnprocessableEntity)
				if !hx && !strings.Contains(html.UnescapeString(response.Body.String()), input) {
					t.Fatal("rejected Save lost the component")
				}
				fields.Set("title", "Social review")
				status := http.StatusSeeOther
				if hx {
					status = http.StatusOK
				}
				requireStatus(t, h.request("POST", "/post", fields, hx), status)
				post, err := h.blog.posts.GetBySlug(t.Context(), "social-review", true)
				if err != nil {
					t.Fatal(err)
				}
				publicImage := func() int {
					w := httptest.NewRecorder()
					h.app.Handler.ServeHTTP(w, httptest.NewRequest("GET", "https://blog.example/images/"+image, nil))
					return w.Code
				}
				if publicImage() != http.StatusNotFound {
					t.Fatal("draft screenshot became public")
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
				if err != nil || publicImage() != http.StatusOK {
					t.Fatal("published screenshot unavailable", err)
				}
				post.Markdown, post.Draft = "Component removed", true
				post, err = savePostFixture(h.blog.posts, t.Context(), post)
				if err != nil {
					t.Fatal(err)
				}
				usage, err := h.blog.mediaReferences.ReferencedMedia(t.Context(), image)
				if err != nil || publicImage() != http.StatusNotFound || usage[image].Current || len(usage[image].History) == 0 {
					t.Fatal("removed screenshot must remain private in history", usage, err)
				}
				if err := h.blog.restoreRevisionContent(adminWritingContext(t.Context()), "post", post.ID, post.Version, original); err != nil {
					t.Fatal(err)
				}
				restored, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
				if err != nil || restored.Markdown != original.Markdown {
					t.Fatal("restoration lost component data", restored, err)
				}
			})
		}
	}
}

func TestSocialPostPermissions(t *testing.T) {
	for _, test := range append(socialPostCases(), additionalSocialPostCases()...) {
		t.Run(test.provider.Name, func(t *testing.T) {
			h := usersHarness(t)
			admin, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
			if err != nil {
				t.Fatal(err)
			}
			poster := registeredPoster(t, h)
			image := mediaUpload(t, h.blog, "Private social screenshot").ID
			if err := h.blog.mediaMetadata.RememberMedia(t.Context(), image, mediaMetadata{Filename: "private.png", UploadedBy: admin.ID}); err != nil {
				t.Fatal(err)
			}
			fields := url.Values{"title": {"Poster social post"}, "socialPost": {componentJSON(postSocialPost{URL: test.url, ImageID: image})}, "componentOrder": {componentJSON([]string{"social-post"})}}
			for _, hx := range []bool{false, true} {
				requireStatus(t, h.request("POST", "/post", fields, hx), http.StatusUnprocessableEntity)
			}
			ownImage := mediaUpload(t, h.blog, "Poster social screenshot").ID
			if err := h.blog.mediaMetadata.RememberMedia(t.Context(), ownImage, mediaMetadata{Filename: "own.png", UploadedBy: poster.ID}); err != nil {
				t.Fatal(err)
			}
			fields.Set("socialPost", componentJSON(postSocialPost{URL: test.url, ImageID: ownImage}))
			requireStatus(t, h.request("POST", "/post", fields, false), http.StatusSeeOther)
			post, err := h.blog.posts.GetBySlug(t.Context(), "poster-social-post", true)
			if err != nil || post.AuthorID != poster.ID {
				t.Fatal("poster could not save their own component", post, err)
			}
		})
	}
}

func TestUnifiedSocialPost(t *testing.T) {
	cases := append(append(socialPostCases(), additionalSocialPostCases()...), struct {
		provider socialProvider
		url      string
	}{xProvider(), "https://twitter.com/user/status/123?s=20"})
	for _, test := range cases {
		t.Run(test.provider.Name, func(t *testing.T) {
			value := postSocialPost{URL: test.url, Description: "Keep this context.", ImageID: strings.Repeat("b", 64) + ".png"}
			input := componentInput{"socialPost": componentJSON(value), "componentOrder": `["social-post"]`}
			source, err := joinPostComponents("Body", input)
			if err != nil {
				t.Fatal(err)
			}
			_, components, err := splitPostComponents(source)
			if err != nil || !slices.Equal(components.Order(), []string{"social-post"}) {
				t.Fatal("all platforms must use the same component", components, err)
			}
			post := components.Components[0].Content.(*postSocialPost)
			if post.provider.Kind != test.provider.Kind || post.EditorSource() != input["socialPost"] {
				t.Fatal("the URL must determine the provider without changing authored input", post)
			}
			reader, err := markdown(source)
			if err != nil || !strings.Contains(string(reader), "Open on "+test.provider.Name) {
				t.Fatal("the URL must choose the correct reader", reader, err)
			}
			page := editorPage{Post: Post{Markdown: source}}
			if page.componentFields()["socialPost"] != input["socialPost"] {
				t.Fatal("reopening lost the unified source")
			}
		})
	}
	for _, source := range []string{
		`{"url":"https://example.test/post/123"}`,
		`{"url":"https://x.com/user/status/123","provider":"facebook-post"}`,
		`{"url":"https://bsky.app/profile/user.bsky.social"}`,
		`{"url":"https://www.threads.com/@example/post/C1234567890","html"
 "errors":"<script>"}`,
	} {
		if _, err := parseSocialPost(source); err == nil {
			t.Fatal("invalid unified social post accepted", source)
		}
	}
}

func TestLegacySocialPostEditor(t *testing.T) {
	h := registeredBrowserHarness(t)
	h.login(t)
	cases := append(socialPostCases(), struct {
		provider socialProvider
		url      string
	}{xProvider(), "https://x.com/user/status/123"})
	for _, test := range cases {
		t.Run(test.provider.Name, func(t *testing.T) {
			source, err := joinPostComponents("Keep this writing.", componentInput{
				test.provider.Field: componentJSON(postSocialPost{URL: test.url, Description: "Keep this context."}),
				"componentOrder":    componentJSON([]string{test.provider.Kind}),
			})
			if err != nil {
				t.Fatal(err)
			}
			post, err := h.blog.persistPost(t.Context(), Post{Title: "Legacy " + test.provider.Name, Markdown: source, Draft: true})
			if err != nil {
				t.Fatal(err)
			}
			response := h.request("GET", "/post/update/"+post.ID, nil, false)
			requireStatus(t, response, http.StatusOK)
			body := response.Body.String()
			if strings.Count(body, `data-component-add="social-post"`) != 1 {
				t.Fatal("the picker must offer one Social post option")
			}
			for _, legacy := range cases {
				if strings.Contains(body, `data-component-add="`+legacy.provider.Kind+`"`) {
					t.Fatal("the picker must not offer a provider-specific component")
				}
			}
			// A saved provider-specific component can now change platforms without
			// changing its envelope key or losing context and revision history.
			changedURL := "https://www.threads.com/@example/post/C1234567890"
			if test.provider.Kind == "threads-post" {
				changedURL = "https://x.com/user/status/123"
			}
			fields := url.Values{
				"title": {post.Title}, "slug": {post.Slug}, "content": {"Keep this writing."}, "version": {strconv.FormatInt(post.Version, 10)},
				test.provider.Field: {componentJSON(postSocialPost{URL: changedURL, Description: "Keep this context."})},
				"componentOrder":    {componentJSON([]string{test.provider.Kind})},
			}
			requireStatus(t, h.request("POST", "/post/"+post.ID, fields, true), http.StatusOK)
			saved, err := h.blog.posts.GetByID(t.Context(), post.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			_, components, err := splitPostComponents(saved.Markdown)
			if err != nil || !slices.Equal(components.Order(), []string{test.provider.Kind}) {
				t.Fatal("the existing stored kind must remain supported", components, err)
			}
			value := components.Components[0].Content.(*postSocialPost)
			if value.URL != changedURL || value.Description != "Keep this context." || saved.Version <= post.Version {
				t.Fatal("changing platforms lost content or history", saved, value)
			}
			reader, err := markdown(saved.Markdown)
			if err != nil || !strings.Contains(string(reader), "Open on "+value.provider.Name) {
				t.Fatal("legacy storage must render the newly detected platform", reader, err)
			}
		})
	}
}

func TestSocialPostChoiceConflicts(t *testing.T) {
	cases := append(socialPostCases(), struct {
		provider socialProvider
		url      string
	}{xProvider(), "https://x.com/user/status/123"})
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "htmx"}[hx], func(t *testing.T) {
			h := registeredBrowserHarness(t)
			h.login(t)
			for _, test := range cases {
				t.Run(test.provider.Name, func(t *testing.T) {
					canonical := componentJSON(postSocialPost{URL: "https://www.threads.com/@example/post/C1234567890", Description: "Replacement context."})
					legacy := componentJSON(postSocialPost{URL: test.url, Description: "Original context."})
					slug := "choice-conflict-" + test.provider.Kind
					fields := url.Values{"title": {"Keep both inputs"}, "slug": {slug}, "content": {"Keep this writing."}, "socialPost": {canonical}, test.provider.Field: {legacy}, "componentOrder": {componentJSON([]string{test.provider.Kind, "social-post"})}}
					response := h.request("POST", "/post", fields, hx)
					requireStatus(t, response, http.StatusUnprocessableEntity)
					if !strings.Contains(response.Body.String(), `data-error-fields="socialPost"`) || !strings.Contains(response.Body.String(), "Keep one social media component.") {
						t.Fatal("a conflicting save must identify the shared choice", response.Body.String())
					}
					if !hx {
						body := html.UnescapeString(response.Body.String())
						for _, input := range []string{canonical, legacy, "Keep this writing."} {
							if !strings.Contains(body, input) {
								t.Fatal("the rejected save lost submitted input", input)
							}
						}
					}
					if _, err := h.blog.posts.GetBySlug(t.Context(), slug, true); !errors.Is(err, errNotFound) {
						t.Fatal("a conflicting component was saved", err)
					}
					fields.Set(test.provider.Field, "")
					fields.Set("componentOrder", `["social-post"]`)
					status := http.StatusSeeOther
					if hx {
						status = http.StatusOK
					}
					requireStatus(t, h.request("POST", "/post", fields, hx), status)
					saved, err := h.blog.posts.GetBySlug(t.Context(), slug, true)
					if err != nil {
						t.Fatal(err)
					}
					_, components, err := splitPostComponents(saved.Markdown)
					if err != nil || !slices.Equal(components.Order(), []string{"social-post"}) || components.Components[0].Content.EditorSource() != canonical {
						t.Fatal("removing the old choice must save the replacement intact", saved, err)
					}
				})
			}
		})
	}
}
