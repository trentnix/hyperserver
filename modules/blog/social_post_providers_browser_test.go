package blog

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBrowserAdditionalSocialPosts(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check additional social platforms")
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pending.gif" {
			<-r.Context().Done()
			return
		}
		if r.URL.Path == "/widget.js" {
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `const path = location.pathname;
const kind = path.startsWith('/r/') ? 'reddit' : path.startsWith('/p/') ? 'instagram' : path.startsWith('/player/') ? 'tiktok' : path.startsWith('/embed/feed/') ? 'linkedin' : 'mastodon';
let id, rendered = false;
function send(height, bad = false) {
 const value = kind === 'reddit' ? JSON.stringify({type:'resize.embed',data:height}) : kind === 'instagram' ? JSON.stringify({type:'MEASURE',details:{height}}) : kind === 'tiktok' ? {'x-tiktok-player':true,type:'onPlayerReady'} : {type:'setHeight',id:bad ? 0 : id,height};
 parent.postMessage(value, '*');
}
document.body.append('A public post and its media');
window.addEventListener('message', event => {
 if (event.source !== parent && event.source !== parent.parent) return;
 if (event.data?.type === 'setHeight') {
  id = event.data.id;
  if (rendered) send(Math.max(innerHeight, innerWidth < 400 ? 500 : 220.4));
 }
 if (event.data === 'test:ready' && (kind !== 'mastodon' || id !== undefined)) event.source.postMessage('test:ready', event.origin);
 if (event.data === 'test:render') { rendered = true; send(kind === 'mastodon' && innerWidth < 400 ? 500 : 220.4); }
 if (event.data === 'test:dimensions') parent.postMessage(JSON.stringify({signalSource:'',width:325,height:578}), '*');
 if (event.data === 'test:bad-id') send(777, true);
 if (event.data?.resize !== undefined) send(event.data.resize);
 if (event.data === 'test:error' && kind === 'tiktok') parent.postMessage({'x-tiktok-player':true,type:'onPlayerError',value:{errorCode:1001,errorType:'INVALID_VIDEO'}}, '*');
});`)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(75 * time.Millisecond):
		}
		w.Header().Set("Content-Type", "text/html")
		if strings.HasSuffix(r.URL.Path, ":999998") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "<!doctype html><title>Post unavailable</title><p>This LinkedIn post is unavailable.</p>")
			return
		}
		pending := ""
		if r.URL.Path == "/player/v1/999999" || strings.HasSuffix(r.URL.Path, ":999999") {
			pending = `<img src="/pending.gif" alt="">`
		}
		fmt.Fprintf(w, `<!doctype html><html><head><script src="/widget.js" defer></script></head><body>%s</body></html>`, pending)
	}))
	defer provider.Close()
	for _, width := range []string{"320", "1100"} {
		for _, sample := range additionalSocialPostCases() {
			t.Run(width+"/"+sample.provider.Name, func(t *testing.T) {
				testBrowser(t, browser, width, browserScenario{
					scriptFile: "testdata/social-post-providers.js", startPath: "/post/s/social-review", authenticated: true, frameOrigin: provider.URL,
					setup: func(t *testing.T) *harness {
						h := registeredBrowserHarness(t)
						h.login(t)
						image := mediaUpload(t, h.blog, "Social fallback").ID
						source, err := joinPostComponents("Review", componentInput{"socialPost": componentJSON(postSocialPost{URL: sample.url, ImageID: image}), "componentOrder": `["social-post"]`})
						if err != nil {
							t.Fatal(err)
						}
						if _, err := h.blog.persistPost(t.Context(), Post{Title: "Social review", Slug: "social-review", Markdown: source}); err != nil {
							t.Fatal(err)
						}
						return h
					},
					routes: func(h *harness) map[string]http.HandlerFunc {
						return map[string]http.HandlerFunc{
							"/assets/js/social-post.js": func(w http.ResponseWriter, r *http.Request) {
								data, err := blogModuleFS.ReadFile("static/js/social-post.js")
								if err != nil {
									t.Fatal(err)
								}
								script := strings.ReplaceAll(string(data), "https://graph.facebook.com/v27.0/instagram_oembed", "/test/social-oembed")
								for _, origin := range []string{"https://embed.reddit.com", "https://www.instagram.com", "https://www.linkedin.com", "https://www.tiktok.com"} {
									script = strings.ReplaceAll(script, origin, provider.URL)
								}
								script = strings.ReplaceAll(script, "return url + 'embed/captioned/'", "return providers['instagram-post'].origin + new URL(url).pathname + 'embed/captioned/'")
								w.Header().Set("Content-Type", "text/javascript")
								fmt.Fprint(w, script)
							},
							"/test/social-oembed": func(w http.ResponseWriter, r *http.Request) {
								w.Header().Set("Content-Type", "application/json")
								fmt.Fprint(w, `{"type":"rich","html":"<blockquote>Ignored HTML</blockquote>"}`)
							},
							"/social/mastodon": func(w http.ResponseWriter, r *http.Request) {
								response := httptest.NewRecorder()
								h.blog.mastodonEmbed(response, r)
								w.Header().Set("Content-Type", "text/html")
								w.Header().Set("X-Frame-Options", "SAMEORIGIN")
								w.Header().Set("Content-Security-Policy", strings.ReplaceAll(response.Header().Get("Content-Security-Policy"), "https://mastodon.social", provider.URL))
								fmt.Fprint(w, strings.ReplaceAll(response.Body.String(), "https://mastodon.social", provider.URL))
							},
						}
					},
				})
			})
		}
	}
}
