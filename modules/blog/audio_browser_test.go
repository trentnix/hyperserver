package blog

import (
	"encoding/base64"
	"fmt"
	"os"
	"testing"
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
