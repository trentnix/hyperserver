package blog

import (
	"encoding/base64"
	"fmt"
	"os"
	"testing"
)

func TestBrowserPDF(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check PDF previews")
	}
	for _, width := range []string{"390", "1100", "3440"} {
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
					return append([]byte(fmt.Sprintf("const testPDFData = %q;\n", base64.StdEncoding.EncodeToString(data))), script...)
				},
			})
		})
	}
}
