package blog

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestBrowserComponentStatus(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check component save indicators")
	}
	for _, width := range []string{"320", "390", "1100", "3440"} {
		t.Run(width, func(t *testing.T) {
			testBrowser(t, browser, width, browserScenario{
				scriptFile: "testdata/component-status.js", startPath: "/post/create", authenticated: true,
				setup: func(t *testing.T) *harness {
					h := registeredBrowserHarness(t)
					h.login(t)
					photo := mediaUpload(t, h.blog, "Saved photo")
					file := testDownload(t, h, "saved.txt", "Saved download")
					pdf := testPDF(t, h)
					audio := testAudio(t, h)
					fields := url.Values{
						"audio": {audioJSON(t, audio)},
						"title": {"Component status"}, "slug": {"component-status"}, "content": {"Body"},
						"album": {"![Saved caption](/images/" + photo.ID + ")"}, "downloads": {downloadSource(t, file)}, "pdf": {downloadSource(t, pdf)},
						"link":  {linkSource(t, postLink{URL: "https://example.com/", Title: "Saved link"})},
						"video": {videoJSON(t, postVideo{URL: "https://youtu.be/M7lc1UVf-VE", Title: "Earlier video"})},
					}
					requireStatus(t, h.request("POST", "/post", fields, false), http.StatusSeeOther)
					post, err := h.blog.posts.GetBySlug(t.Context(), "component-status", true)
					if err != nil {
						t.Fatal(err)
					}
					fields.Set("video", videoJSON(t, postVideo{URL: "https://youtu.be/M7lc1UVf-VE"}))
					requireStatus(t, h.submitEditor(t, "/post/"+post.ID, fields, false), http.StatusSeeOther)
					return h
				},
				prepareScript: func(t *testing.T, h *harness, script []byte) []byte {
					post, err := h.blog.posts.GetBySlug(t.Context(), "component-status", true)
					if err != nil {
						t.Fatal(err)
					}
					versions, err := h.blog.history.ListRevisions(t.Context(), "post", post.ID)
					if err != nil || len(versions) != 2 {
						t.Fatal("expected two saved versions", err)
					}
					oldest := versions[len(versions)-1]
					paths, _ := json.Marshal(map[string]string{"editor": "/post/update/" + post.ID, "history": "/post/update/" + post.ID + "?revision=" + strconv.FormatInt(oldest.ID, 10)})
					return []byte(strings.ReplaceAll(string(script), "__EDITOR_PATHS__", string(paths)))
				},
			})
		})
	}
}
