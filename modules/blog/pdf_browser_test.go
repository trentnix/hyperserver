package blog

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
)

func TestBrowserPDF(t *testing.T) { testPDFBrowser(t, false) }

func TestBrowserPDFPresentation(t *testing.T) { testPDFBrowser(t, true) }

func testPDFBrowser(t *testing.T, presentation bool) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check PDF previews")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/pdf.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					testPDF(t, h)
					testDownload(t, h, "Not a PDF.txt", "Only PDF files belong in the preview library")
					return h
				},
				prepareScript: func(t *testing.T, _ *harness, script []byte) []byte {
					data, err := os.ReadFile("testdata/preview.pdf")
					if err != nil {
						t.Fatal(err)
					}
					return append([]byte(fmt.Sprintf("const testPDFData = %q;\nconst testPresentationMode = %t;\n", base64.StdEncoding.EncodeToString(data), presentation)), script...)
				},
			})
		})
	}
}

func TestBrowserPDFPresentationRetry(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check PDF presentation recovery")
	}
	var file postDownload
	testBrowser(t, browser, "320", browserScenario{
		scriptFile: "testdata/pdf-presentation-retry.js", startPath: "/post/s/presentation-retry", authenticated: true, allowFullscreen: true,
		setup: func(t *testing.T) *harness {
			h := registeredBrowserHarness(t)
			h.login(t)
			file = testPDF(t, h)
			file.Presentation = true
			source, err := joinPostComponents("", componentInput{"pdf": downloadSource(t, file)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.blog.persistPost(t.Context(), Post{Title: "Presentation retry", Slug: "presentation-retry", Markdown: source}); err != nil {
				t.Fatal(err)
			}
			return h
		},
		routes: func(h *harness) map[string]http.HandlerFunc {
			var failed atomic.Bool
			return map[string]http.HandlerFunc{
				"/files/" + file.ID: func(w http.ResponseWriter, r *http.Request) {
					if !failed.Swap(true) {
						http.Error(w, "Temporary failure", http.StatusServiceUnavailable)
						return
					}
					h.app.Handler.ServeHTTP(w, r)
				},
				"/assets/lib/pdfjs/build/pdf.min.mjs": func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Query().Has("original") {
						h.app.Handler.ServeHTTP(w, r)
						return
					}
					http.ServeFile(w, r, "testdata/pdf-presentation-probe.js")
				},
			}
		},
	})
}
