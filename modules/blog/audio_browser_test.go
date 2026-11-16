package blog

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestBrowserAudio(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check audio components")
	}
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/audio.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					testAudio(t, h)
					testPDF(t, h)
					testDownload(t, h, "Notes.txt", "Not an MP3")
					return h
				},
				routes: func(h *harness) map[string]http.HandlerFunc {
					return map[string]http.HandlerFunc{"/post/files": func(w http.ResponseWriter, r *http.Request) {
						// A slow metadata response must not expose the storage ID.
						select {
						case <-r.Context().Done():
							return
						case <-time.After(300 * time.Millisecond):
						}
						h.app.Handler.ServeHTTP(w, r)
					}}
				},
				prepareScript: func(t *testing.T, _ *harness, script []byte) []byte {
					data, err := os.ReadFile("testdata/audio.mp3")
					if err != nil {
						t.Fatal(err)
					}
					return append([]byte(fmt.Sprintf("const testMP3Data = %q;\n", base64.StdEncoding.EncodeToString(data))), script...)
				},
			})
		})
	}
}
