package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/handlers"
)

type startupTemplateFS struct {
	fs.FS
	serving   atomic.Bool
	lateReads atomic.Int64
}

func (f *startupTemplateFS) Open(name string) (fs.File, error) {
	if f.serving.Load() {
		f.lateReads.Add(1)
		return nil, fmt.Errorf("template read after startup: %s", name)
	}
	return f.FS.Open(name)
}

func TestHTTPTemplatesPreparedBeforeRequests(t *testing.T) {
	var filesystems []*startupTemplateFS
	catalog := func(app *application) ([]handlers.Descriptor, error) {
		cm := app.ContentManager
		// Wrap every shared template source before any module initializes.
		for _, paths := range []map[string][]content.TemplatePath{cm.Layouts, cm.Contents, cm.Components} {
			for kind, files := range paths {
				for i := range files {
					filesys := &startupTemplateFS{FS: files[i].FS}
					files[i].FS = filesys
					filesystems = append(filesystems, filesys)
				}
				paths[kind] = files
			}
		}
		return siteModules(app, "")
	}
	runHTTPScenarioWithCatalog(t, 30*time.Second, catalog, func(h *httpHarness) {
		if len(filesystems) == 0 {
			t.Fatal("no template filesystems instrumented")
		}
		for _, files := range filesystems {
			files.serving.Store(true)
		}
		h.app.Web.HandleFunc("GET /test/prepared-message", func(w http.ResponseWriter, r *http.Request) {
			h.app.ContentManager.HandleMessage(w, r, "prepared message")
		})
		h.app.Web.HandleFunc("GET /test/prepared-error", func(w http.ResponseWriter, r *http.Request) {
			h.app.ContentManager.HandleError(w, r, "prepared error", nil, http.StatusForbidden)
		})
		for _, htmx := range []bool{false, true} {
			for _, path := range []string{"/", "/login", "/register", "/contact", "/auth/login/email", "/auth/register/email", "/auth/reset/request/email", "/missing", "/test/prepared-message", "/test/prepared-error"} {
				w := h.request(http.MethodGet, path, nil, htmx)
				want := http.StatusOK
				if path == "/missing" {
					want = http.StatusNotFound
				}
				if path == "/test/prepared-error" {
					want = http.StatusForbidden
				}
				if w.Code != want || strings.TrimSpace(w.Body.String()) == "" {
					t.Fatalf("htmx=%t %s: status=%d body=%s", htmx, path, w.Code, w.Body.String())
				}
			}
		}
		account := h.seedUser(t, "verify@example.invalid")
		account.VerificationRequired = true
		h.establishSession(t, account)
		for _, htmx := range []bool{false, true} {
			w := h.request(http.MethodGet, "/auth/request/verify", nil, htmx)
			if w.Code != http.StatusOK {
				t.Fatalf("verification status=%d body=%s", w.Code, w.Body.String())
			}
		}
		for _, files := range filesystems {
			if reads := files.lateReads.Load(); reads != 0 {
				t.Fatalf("requests attempted %d template reads", reads)
			}
		}
	})
}
