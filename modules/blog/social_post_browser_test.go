package blog

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBrowserSocialPosts(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check social post components")
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/widget.js" {
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `const query = new URLSearchParams(location.search);
const isFacebook = location.pathname === '/plugins/post.php';
const isThreads = location.pathname.endsWith('/embed/');
const id = isFacebook ? new URLSearchParams(new URL(query.get('channel')).hash.slice(1)).get('cb') : query.get('id');
let isolated = false; try { parent.document.body; } catch { isolated = true; }
if (!isolated) throw new Error('Provider must have a separate origin');
document.body.style.margin = '0'; document.body.textContent = 'A public test post and its media';
function send(height, badID = false) {
 const callback = badID ? 'wrong' : id;
 const data = isFacebook ? new URLSearchParams({cb:callback,type:'resize',height}).toString() : isThreads ? height : {id:callback,height};
 parent.postMessage(data, '*');
}
window.addEventListener('message', event => {
 if (event.source !== parent) return;
 if (event.data === 'test:render') send(220.4);
 if (event.data === 'test:bad-id') send(777, true);
 if (event.data?.resize !== undefined) send(event.data.resize);
});`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><script src="/widget.js" defer></script></head><body></body></html>`)
	}))
	defer provider.Close()
	for _, width := range []string{"320", "390", "1100", "3440"} {
		for _, platform := range []string{"bluesky-post", "threads-post", "facebook-post"} {
			t.Run(width+"/"+platform, func(t *testing.T) {
				testBrowser(t, browser, width, browserScenario{
					scriptFile: "testdata/social-post.js", startPath: "/post/create?platform=" + platform, authenticated: true, frameOrigin: provider.URL,
					setup: func(t *testing.T) *harness {
						h := registeredBrowserHarness(t)
						h.login(t)
						mediaUpload(t, h.blog, "Social post screenshot")
						return h
					},
					routes: func(_ *harness) map[string]http.HandlerFunc {
						return map[string]http.HandlerFunc{
							"/assets/js/social-post.js": func(w http.ResponseWriter, r *http.Request) {
								data, err := blogModuleFS.ReadFile("static/js/social-post.js")
								if err != nil {
									t.Error(err)
									return
								}
								script := strings.ReplaceAll(string(data), "https://embed.bsky.app/oembed", "/test/social-oembed")
								script = strings.ReplaceAll(script, "https://graph.threads.com/oembed", "/test/social-oembed")
								script = strings.ReplaceAll(script, "https://graph.facebook.com/v27.0/oembed_post", "/test/social-oembed")
								for _, origin := range []string{"https://embed.bsky.app", "https://www.threads.com", "https://www.facebook.com"} {
									script = strings.ReplaceAll(script, origin, provider.URL)
								}
								// Rewrite the canonical Threads viewing URL only at the iframe boundary.
								script = strings.ReplaceAll(script, "return url + '/embed/'", "return providers['threads-post'].origin + new URL(url).pathname + '/embed/'")
								w.Header().Set("Content-Type", "text/javascript")
								fmt.Fprint(w, script)
							},
							"/test/social-oembed": func(w http.ResponseWriter, r *http.Request) {
								w.Header().Set("Content-Type", "application/json")
								if strings.HasSuffix(r.URL.Query().Get("url"), "/missing") {
									w.WriteHeader(http.StatusNotFound)
									fmt.Fprint(w, `{"error":"Post unavailable"}`)
									return
								}
								fmt.Fprint(w, `{"type":"rich","html":"<blockquote data-bluesky-uri=\"at://did:plc:z72i7hdynmk6r22z27h6tvur/app.bsky.feed.post/3kabc12345678\"></blockquote><script src=\"https://evil.test/script.js\"></script>"}`)
							},
						}
					},
				})
			})
		}
	}
}
