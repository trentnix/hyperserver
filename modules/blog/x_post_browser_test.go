package blog

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBrowserXPost(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check X post components")
	}
	// A separate origin exercises the real iframe boundary and message handling.
	// It stands in for the provider so tests need no X credentials or network.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/widget.js" {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write([]byte(`const id = new URLSearchParams(location.search).get('id');
let isolated = false;
try { parent.document.body; } catch { isolated = true; }
if (!isolated) throw new Error('Provider must have a separate origin');
const send = (method, details) => parent.postMessage({'twttr.embed': {jsonrpc: '2.0', method: 'twttr.private.' + method, params: [details]}}, '*');
if (id === '2') send('no_results', {});
else if (id !== '3') {
 document.documentElement.style.overflowY = 'scroll';
 document.body.style.margin = '0';
 document.body.style.height = '220.4px';
 document.body.textContent = 'A test X post and its media';
 send('resize', {height: 220});
 if (id !== '5') window.addEventListener('message', event => {
  if (event.source === parent && event.data?.resize) send('resize', {height: event.data.resize});
  if (event.source === parent && event.data === 'test:render') send('rendered', {viewport: innerWidth, content: document.documentElement.clientWidth});
 });
}`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><html><head><title>Test X post</title><script src="/widget.js" defer></script></head><body></body></html>`))
	}))
	defer provider.Close()
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/x-post.js", startPath: "/post/create", authenticated: true, frameOrigin: provider.URL,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					mediaUpload(t, h.blog, "X post screenshot")
					return h
				},
				routes: func(_ *harness) map[string]http.HandlerFunc {
					return map[string]http.HandlerFunc{"/assets/js/x-post.js": func(w http.ResponseWriter, r *http.Request) {
						data, err := blogModuleFS.ReadFile("static/js/x-post.js")
						if err != nil {
							t.Error(err)
							return
						}
						w.Header().Set("Content-Type", "text/javascript")
						w.Write([]byte(strings.ReplaceAll(string(data), "https://platform.twitter.com", provider.URL)))
					}}
				},
			})
		})
	}
}
