package blog

import (
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestMarkdownImageSize(t *testing.T) {
	const limitImageHeight = "{size=limit-height}"
	for _, tc := range []struct {
		name, source, want, size string
	}{
		{"default", "![Diagram](/image.png)", `<img src="/image.png" alt="Diagram" data-markdown-image="">`, ""},
		{"limited", limitImageHeight + "![Diagram](/image.png)", `<img`, "limit-height"},
		{"unicode", "Café 🌱 " + limitImageHeight + "![Diagram](/image.png)", `Café 🌱 <img`, "limit-height"},
		{"linked reference", "[" + limitImageHeight + "![Diagram][figure]](/page/example)\n\n[figure]: /image.png", `<a href="/page/example"><img`, "limit-height"},
		{"escaped", `\` + limitImageHeight + "![Diagram](/image.png)", limitImageHeight + "<img", ""},
		{"separated", limitImageHeight + " ![Diagram](/image.png)", limitImageHeight + " <img", ""},
		{"code", "`" + limitImageHeight + "![Diagram](/image.png)`", "<code>" + limitImageHeight, ""},
		{"compact", "{size=compact}![Diagram](/image.png)", `<img`, "compact"},
		{"medium", "{size=medium}![Diagram](/image.png)", `<img`, "medium"},
		{"width only", "{size=custom max-width=480}![Diagram](/image.png)", `style="--markdown-image-max-width:480px;"`, "custom"},
		{"height only", "{size=custom max-height=4096}![Diagram](/image.png)", `style="--markdown-image-max-height:4096px;"`, "custom"},
		{"both limits", "{size=custom max-width=260 max-height=540}![Diagram](/image.png)", `style="--markdown-image-max-width:260px;--markdown-image-max-height:540px;"`, "custom"},
		{"too large", "{size=custom max-width=4097}![Diagram](/image.png)", `{size=custom max-width=4097}<img`, ""},
		{"style declaration", "{size=custom max-width=400px;color:red}![Diagram](/image.png)", `{size=custom max-width=400px;color:red}<img`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html, err := markdown(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			got := string(html)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("rendered %s, want %s", got, tc.want)
			}
			if tc.size == "" && strings.Contains(got, `data-markdown-image-size=`) ||
				tc.size != "" && (!strings.Contains(got, `data-markdown-image-size="`+tc.size+`"`) || strings.Contains(got, "{size=")) {
				t.Fatalf("unexpected size marker rendering: %s", got)
			}
		})
	}
}

func TestMarkdownImagePreview(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	source := "Café 🌱\n\n{size=compact}![One][picture]\n\n![Two](javascript:example)\n\n[picture]: /diagram.png"
	response := h.request("POST", "/post/preview", url.Values{"content": {source}, "imageSettings": {""}}, false)
	requireStatus(t, response, http.StatusOK)
	got := response.Body.String()
	first := strconv.Itoa(strings.Index(source, "![One]"))
	second := strconv.Itoa(strings.Index(source, "![Two]"))
	want := `<img src="/diagram.png" alt="One" data-markdown-image="` + first + `" data-markdown-image-size="compact">` +
		`<img src="" alt="Two" data-markdown-image="` + second + `">`
	if got != want {
		t.Fatalf("image-only preview = %q, want %q", got, want)
	}
}

func TestBrowserMarkdownImageSize(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check Markdown image sizing")
	}
	for _, width := range []string{"320", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/markdown-image-size.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					return h
				},
			})
		})
	}
}

func TestBrowserMarkdownImages(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check Markdown image enlargement")
	}
	for _, viewport := range []struct {
		name, width string
		height      int
	}{{"320", "320", 1000}, {"1100", "1100", 600}, {"2560", "2560", 1000}, {"short", "1100", 280}} {
		t.Run(viewport.name, func(t *testing.T) {
			testBrowser(t, browser, viewport.width, browserScenario{
				scriptFile: "testdata/markdown-images.js", startPath: "/post/s/image-review",
				height: viewport.height,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					writing := "![Wide diagram](/test/image.svg?size=wide)\n\n" +
						"{size=limit-height}![Fit to screen](/test/image.svg?size=tall)\n\n" +
						"[![Linked image](/test/image.svg?size=small)](/page/image-review)\n\n" +
						"![Invalid image](javascript:example)"
					body, err := joinPostComponents(writing, componentInput{
						"callout":        `{"body":"![Callout screenshot](/test/image.svg?size=small)"}`,
						"componentOrder": `["callout"]`,
					})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := h.blog.persistPost(t.Context(), Post{
						Slug: "image-review", Title: "Markdown images", Markdown: body,
					}); err != nil {
						t.Fatal(err)
					}
					if _, err := h.blog.persistPage(t.Context(), Page{
						Slug: "image-review", Title: "Page image", Published: true, ChildrenDisplay: "hidden",
						Markdown: "![](/test/image.svg?size=tiny)",
					}); err != nil {
						t.Fatal(err)
					}
					return h
				},
			})
		})
	}
}
