package blog

import (
	"os"
	"strings"
	"testing"
)

func TestBrowserFullUploadStorage(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check upload controls at the storage limit")
	}
	for _, path := range []string{"/post/create", "/pages/create", "/profile", "/post/create?fill=1"} {
		t.Run(path, func(t *testing.T) {
			testBrowser(t, browser, "390", browserScenario{
				scriptFile: "testdata/upload-quota.js", startPath: path, authenticated: true,
				setup: func(t *testing.T) *harness {
					h := usersHarness(t)
					h.blog.settings.FileStorageLimitMiB = 1
					testDownload(t, h, "Existing document.pdf", "%PDF-1.4\n%%EOF")
					store := h.blog.downloads.(*fileDownloadStore)
					file, err := store.root.OpenFile(strings.Repeat("c", 64)+".txt", os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
					if err != nil {
						t.Fatal(err)
					}
					size := h.blog.fileStorageLimit()
					if strings.HasSuffix(path, "?fill=1") {
						// Leave exactly three bytes available for the browser upload.
						used, err := h.blog.media.List(t.Context())
						if err != nil {
							t.Fatal(err)
						}
						size -= fileStorageUsed(used) + 3
					}
					err = file.Truncate(size)
					if closeErr := file.Close(); err != nil || closeErr != nil {
						t.Fatal(err, closeErr)
					}
					return h
				},
			})
		})
	}
}
