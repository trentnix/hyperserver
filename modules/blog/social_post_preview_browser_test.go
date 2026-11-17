package blog

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBrowserSocialPostPreview(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check social media editor previews")
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><p>A public post</p><script>
addEventListener('message', event => {
 if (event.source !== parent) return;
 if (event.data === 'test:ready') parent.postMessage('test:ready', event.origin);
 if (event.data === 'test:render') parent.postMessage({type:'resize.embed',data:220}, event.origin);
});
</script>`)
	}))
	defer provider.Close()
	for _, width := range []string{"320", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/social-post-preview.js", startPath: "/post/create", authenticated: true, frameOrigin: provider.URL,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					mediaUpload(t, h.blog, "Social preview fallback")
					input := componentInput{"socialPost": `{"url":"https://x.com/user/status/123"}`, "componentOrder": `["social-post"]`}
					source, err := joinPostComponents("", input)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := h.blog.persistPost(t.Context(), Post{Title: "Saved social preview", Slug: "saved-social-preview", Markdown: source, Draft: true}); err != nil {
						t.Fatal(err)
					}
					return h
				},
				routes: func(h *harness) map[string]http.HandlerFunc {
					script := func(w http.ResponseWriter, r *http.Request) {
						data, err := blogModuleFS.ReadFile("static/" + strings.TrimPrefix(r.URL.Path, "/assets/"))
						if err != nil {
							t.Error(err)
							return
						}
						value := string(data)
						for _, endpoint := range []string{"https://embed.bsky.app/oembed", "https://graph.threads.com/oembed", "https://graph.facebook.com/v27.0/oembed_post", "https://graph.facebook.com/v27.0/instagram_oembed"} {
							value = strings.ReplaceAll(value, endpoint, "/test/social-oembed")
						}
						for _, origin := range []string{"https://platform.twitter.com", "https://embed.bsky.app", "https://www.threads.com", "https://www.facebook.com", "https://embed.reddit.com", "https://www.instagram.com", "https://www.linkedin.com", "https://www.tiktok.com"} {
							value = strings.ReplaceAll(value, origin, provider.URL)
						}
						value = strings.ReplaceAll(value, "return url + '/embed/'", "return providers['threads-post'].origin + new URL(url).pathname + '/embed/'")
						value = strings.ReplaceAll(value, "return url + 'embed/captioned/'", "return providers['instagram-post'].origin + new URL(url).pathname + 'embed/captioned/'")
						w.Header().Set("Content-Type", "text/javascript")
						fmt.Fprint(w, value)
					}
					return map[string]http.HandlerFunc{
						"/assets/js/social-post.js": script,
						"/assets/js/x-post.js":      script,
						"/test/social-preview-data": func(w http.ResponseWriter, r *http.Request) {
							samples := []struct{ Kind, URL string }{{"x-post", "https://x.com/user/status/123"}}
							for _, sample := range append(socialPostCases(), additionalSocialPostCases()...) {
								samples = append(samples, struct{ Kind, URL string }{sample.provider.Kind, sample.url})
							}
							post, err := h.blog.posts.GetBySlug(r.Context(), "saved-social-preview", true)
							if err != nil {
								t.Error(err)
							}
							writeJSON(w, r, http.StatusOK, struct {
								Samples any
								EditURL string
							}{samples, "/post/update/" + post.ID})
						},
						"/test/social-oembed": func(w http.ResponseWriter, r *http.Request) {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprint(w, `{"type":"rich","html":"<blockquote data-bluesky-uri=\"at://did:plc:z72i7hdynmk6r22z27h6tvur/app.bsky.feed.post/3kabc12345678\"></blockquote>"}`)
						},
						"/social/mastodon": func(w http.ResponseWriter, r *http.Request) {
							response := httptest.NewRecorder()
							h.blog.mastodonEmbed(response, r)
							w.Header().Set("Content-Type", "text/html")
							w.Header().Set("Content-Security-Policy", strings.ReplaceAll(response.Header().Get("Content-Security-Policy"), "https://mastodon.social", provider.URL))
							fmt.Fprint(w, strings.ReplaceAll(response.Body.String(), "https://mastodon.social", provider.URL))
						},
					}
				},
			})
		})
	}
}
