package blog

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"testing"
)

func TestBrowserComparisonAspectRatio(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check image comparison sizing")
	}
	const slug = "comparison-sizing"
	for _, tc := range []struct {
		name, path, width         string
		beforeHeight, afterHeight int
	}{
		{"wide post", "/post/s/" + slug, "1200", 72, 72},
		{"narrow page", "/page/" + slug, "320", 72, 160},
		{"tall images", "/post/s/" + slug, "390", 768, 768},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testBrowser(t, browser, tc.width, browserScenario{
				scriptFile: "testdata/comparison-sizing.js", startPath: tc.path, height: 800,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					upload := func(height int, color byte) string {
						pixels := image.NewRGBA(image.Rect(0, 0, 128, height))
						copy(pixels.Pix, []byte{color, 0, 0, 255})
						var data bytes.Buffer
						if err := png.Encode(&data, pixels); err != nil {
							t.Fatal(err)
						}
						id, err := h.blog.images.Put(t.Context(), testImageUpload("image/png", data.Bytes()), nil)
						if err != nil {
							t.Fatal(err)
						}
						return id
					}
					comparison := postComparison{BeforeID: upload(tc.beforeHeight, 50), AfterID: upload(tc.afterHeight, 100)}
					body, err := joinPostComponents("", componentInput{"comparison": componentJSON(comparison)})
					if err != nil {
						t.Fatal(err)
					}
					if tc.path == "/page/"+slug {
						_, err = savePageFixture(h.blog.pageStore, t.Context(), Page{Slug: slug, Title: "Comparison sizing", Markdown: body, Published: true})
					} else {
						_, err = savePostFixture(h.blog.posts, t.Context(), Post{Slug: slug, Title: "Comparison sizing", Markdown: body})
					}
					if err != nil {
						t.Fatal(err)
					}
					return h
				},
			})
		})
	}
}

func TestBrowserComparison(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check image comparisons")
	}
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/comparison.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					mediaUpload(t, h.blog, "Before comparison")
					mediaUpload(t, h.blog, "After comparison")
					return h
				},
			})
		})
	}
}
