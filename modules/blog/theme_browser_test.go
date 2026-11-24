package blog

import (
	"net/http"
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
	for _, sample := range []struct{ theme, savedTheme, width string }{
		{"thanksgiving", "default", "320"}, {"thanksgiving", "christmas", "1100"}, {"thanksgiving", "default", "2560"},
		{"christmas", "default", "320"}, {"christmas", "thanksgiving", "1100"}, {"christmas", "default", "2560"},
	} {
		t.Run(sample.theme+"/"+sample.width, func(t *testing.T) {
			testBrowser(t, browser, sample.width, browserScenario{
				scriptFile:    "testdata/theme-preview.js",
				startPath:     "/?preview-theme=" + sample.theme,
				authenticated: true,
				prepareScript: func(_ *testing.T, _ *harness, script []byte) []byte {
					return append([]byte("const expectedTheme = "+strconv.Quote(sample.theme)+"; const savedTheme = "+strconv.Quote(sample.savedTheme)+";\n"), script...)
				},
				routes: func(h *harness) map[string]http.HandlerFunc {
					return map[string]http.HandlerFunc{"/test/save-theme": func(w http.ResponseWriter, r *http.Request) {
						profile := h.blog.profile.Current()
						profile.Theme = sample.theme
						if err := h.blog.persistProfile(r.Context(), profile); err != nil {
							t.Error(err)
							w.WriteHeader(http.StatusInternalServerError)
							return
						}
						w.WriteHeader(http.StatusNoContent)
					}}
				},
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					if sample.savedTheme != "default" {
						profile := h.blog.profile.Current()
						profile.Theme = sample.savedTheme
						if err := h.blog.persistProfile(t.Context(), profile); err != nil {
							t.Fatal(err)
						}
					}
					body, err := joinPostComponents("A paragraph for comparing reading typography.", componentInput{
						"details": componentJSON(postDetails{Title: "More detail", Body: "Step-by-step notes."}),
					})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := savePostFixture(h.blog.posts, t.Context(), Post{Slug: "theme-preview", Title: "A sample article", Markdown: body, Published: time.Now().Add(-time.Hour)}); err != nil {
						t.Fatal(err)
					}
					h.blog.invalidatePosts()
					return h
				},
			})
		})
	}
}
