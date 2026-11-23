package blog

import (
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestMarkdownImportImages(t *testing.T) {
	const uploaded = "/images/uploaded.png"
	const definition = `[figure]: a/../image%20file.png 'A "quote" &amp; more'`
	for _, tc := range []struct {
		name, source, want string
		paths              []string
	}{
		{
			name:   "sized linked image with Unicode and an angled path",
			source: "Café 🌱 [`{size=compact}`{size=custom max-height=480}![A \\[label\\]](<./figures/first image.png> 'Title')](/about)",
			want:   "Café 🌱 [`{size=compact}`{size=custom max-height=480}![A \\[label\\]](<" + uploaded + "> 'Title')](/about)",
			paths:  []string{"figures/first image.png"},
		},
		{
			name:   "reference forms and a link to the same reference",
			source: "[Read][figure]\n\n![Photo][figure] ![figure][] ![figure]\n\n" + definition,
			want:   "[Read][figure]\n\n![Photo](<" + uploaded + `> "A &#34;quote&#34; &amp; more") ![figure](<` + uploaded + `> "A &#34;quote&#34; &amp; more") ![figure](<` + uploaded + `> "A &#34;quote&#34; &amp; more")` + "\n\n" + definition,
			paths:  []string{"image file.png", "image file.png", "image file.png"},
		},
		{
			name:   "blockquote with multiline image",
			source: "> ![A\n> label](\n> path/photo.png\n> \"My title\"\n> )",
			want:   "> ![A\n> label](\n> <" + uploaded + ">\n> \"My title\"\n> )",
			paths:  []string{"path/photo.png"},
		},
		{
			name:   "escaped path, query, and fragment",
			source: `![File](folder/picture\(a\).png?variant=thumbnail&test=1#section)`,
			want:   "![File](<" + uploaded + "?variant=thumbnail&amp;test=1#section>)",
			paths:  []string{"folder/picture(a).png"},
		},
		{
			name:   "site and remote images, links, code, and raw HTML",
			source: "![Remote](https://example.com/file.png) ![Site](/images/a.png) ![CDN](//example.com/a.png) [Link](file.png) `![Code](file.png)` <img src=\"file.png\">\n\n```\n![Example](file.png)\n```",
		},
		{
			name:   "image in alt text is not displayed",
			source: "![Parent ![Child](child.png)](parent.png)",
			want:   "![Parent ![Child](child.png)](<" + uploaded + ">)",
			paths:  []string{"parent.png"},
		},
		{
			name:   "reference at end of file",
			source: "[figure]: x.png\n\n![figure]",
			want:   "[figure]: x.png\n\n![figure](<" + uploaded + ">)",
			paths:  []string{"x.png"},
		},
		{
			name:   "directory destinations are local references",
			source: "![Current](.) ![Parent](..) ![Directory](folder/)",
			want:   "![Current](<" + uploaded + ">) ![Parent](<" + uploaded + ">) ![Directory](<" + uploaded + ">)",
			paths:  []string{".", "..", "folder/"},
		},
		{
			name:   "component format example inside a quote",
			source: "> ```post-components\n> Example\n> ```\n>\n> <!-- hyperserver:component event -->",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edits, err := markdownImportImages([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			var result strings.Builder
			var paths []string
			cursor := 0
			for _, edit := range edits {
				result.WriteString(tc.source[cursor:edit.Start] + edit.Before + uploaded + edit.After)
				cursor = edit.End
				paths = append(paths, edit.Path)
			}
			result.WriteString(tc.source[cursor:])
			want := tc.want
			if want == "" {
				want = tc.source
			}
			if result.String() != want || !slices.Equal(paths, tc.paths) {
				t.Fatalf("got %q, paths %q\nwant %q, paths %q", result.String(), paths, want, tc.paths)
			}
		})
	}
}

func TestPrepareMarkdownImport(t *testing.T) {
	h := registeredBrowserHarness(t)
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "https://blog.example/post/markdown-import", strings.NewReader(body))
		r.Header.Set("Content-Type", "text/plain; charset=utf-8")
		for _, cookie := range h.cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		return w
	}
	requireStatus(t, request("Hello"), http.StatusForbidden)
	h.login(t)
	for _, body := range []string{"", " \r\n ", "\xff", "hello\x00world"} {
		requireStatus(t, request(body), http.StatusUnprocessableEntity)
	}
	for _, body := range []string{
		"Text\n\n```post-components\n{\"version\":2,\"components\":[]}\n```",
		"Text\n\n```photo-album\n\n```",
		"Text\n\n<!-- hyperserver:component event -->",
	} {
		response := request(body)
		requireStatus(t, response, http.StatusUnprocessableEntity)
		if !strings.Contains(response.Body.String(), "Remove the blog component") {
			t.Fatalf("expected a component import error, got %s", response.Body)
		}
	}
	requireStatus(t, request(strings.Repeat("a", maxMarkdownBytes+1)), http.StatusRequestEntityTooLarge)
	requireStatus(t, request(strings.Repeat("a", maxMarkdownBytes)), http.StatusOK)
	response := request("![Image](assets/image.png)")
	requireStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), `"path":"assets/image.png"`) {
		t.Fatalf("unexpected Markdown import response: %s %s", response.Header(), response.Body)
	}
}

func TestBrowserMarkdownImport(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check Markdown import")
	}
	for _, width := range []string{"320", "1100"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/markdown-import.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					return h
				},
			})
		})
	}
}
