package blog

import (
	"os"
	"strconv"
	"testing"
	"time"
)

func TestBrowserThemePreview(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check theme preview")
	}
	for _, sample := range []struct{ theme, width string }{
		{"thanksgiving", "320"}, {"thanksgiving", "1100"}, {"thanksgiving", "2560"},
		{"christmas", "320"}, {"christmas", "1100"}, {"christmas", "2560"},
	} {
		t.Run(sample.theme+"/"+sample.width, func(t *testing.T) {
			testBrowser(t, browser, sample.width, browserScenario{
				scriptFile: "testdata/theme-preview.js",
				startPath:  "/?preview-theme=" + sample.theme,
				prepareScript: func(_ *testing.T, _ *harness, script []byte) []byte {
					return append([]byte("const expectedTheme = "+strconv.Quote(sample.theme)+";\n"), script...)
				},
				setup: func(t *testing.T) *harness {
					h := newHarness(t)
					body, err := joinPostComponents("A paragraph for comparing reading typography.", componentInput{
						"details": componentJSON(postDetails{Title: "More detail", Body: "Step-by-step notes."}),
					})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := savePostFixture(h.blog.posts, t.Context(), Post{Slug: "theme-preview", Title: "A sample article", Markdown: body, Published: time.Now().Add(-time.Hour)}); err != nil {
						t.Fatal(err)
					}
					return h
				},
			})
		})
	}
}
