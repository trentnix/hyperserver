package blog

import (
	"fmt"
	"net/http"
	"os"
	"testing"
)

func TestBrowserMarkdownRequirements(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check Markdown requirements")
	}
	for _, enhanced := range []bool{true, false} {
		t.Run(map[bool]string{true: "enhanced", false: "native"}[enhanced], func(t *testing.T) {
			testBrowser(t, browser, "390", browserScenario{
				scriptFile: "testdata/markdown-requirements.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					return h
				},
				prepareScript: func(t *testing.T, h *harness, script []byte) []byte {
					file := testDownload(t, h, "Optional body.txt", "A component can replace the post body")
					return append([]byte(fmt.Sprintf("const savedFile = %s; const enhanced = %t;\n", downloadSource(t, file), enhanced)), script...)
				},
				routes: func(*harness) map[string]http.HandlerFunc {
					if enhanced {
						return nil
					}
					return map[string]http.HandlerFunc{"/assets/lib/easymde/easymde.min.js": func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "text/javascript")
					}}
				},
			})
		})
	}
}
