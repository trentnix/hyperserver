package web_test

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/htmx"
	"github.com/trentnix/hyperserver/pkg/handlers"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/flash"
)

func TestHTTPRefreshPreservesNotifications(t *testing.T) {
	runSiteScenario(t, func(h *httpHarness) {
		h.app.Web.HandleFunc("POST /test/queue-refresh-notice", func(w http.ResponseWriter, r *http.Request) {
			if err := flash.AddSuccessNotification(w, r, "Retained refresh notification"); err != nil {
				t.Fatal(err)
			}
		})
		h.app.Web.HandleFunc("GET /test/refresh", func(w http.ResponseWriter, r *http.Request) {
			cm := contentservice.NewContentManager()
			cm.LoadNotifications = flash.LoadNotifications
			page := content.NewManagedContent(r, cm)
			page.HTMX.Response = &htmx.Response{Refresh: true}
			page.AddContent(fstest.MapFS{"page.html": {Data: []byte(`{{range .Notifications}}{{.Message}}{{end}}`)}}, "page.html")
			if err := page.Render(w, r); err != nil {
				t.Fatal(err)
			}
		})
		h.request(http.MethodPost, "/test/queue-refresh-notice", nil, false)
		w := h.request(http.MethodGet, "/test/refresh", nil, true)
		if w.Code != http.StatusOK || w.Header().Get(htmx.HeaderRefresh) != "true" || len(w.Result().Cookies()) != 0 {
			t.Fatalf("refresh response: %d %v", w.Code, w.Header())
		}
		next := h.request(http.MethodGet, "/", nil, false)
		if next.Code != http.StatusOK || !strings.Contains(next.Body.String(), "Retained refresh notification") {
			t.Fatal("reload lost the notification")
		}
		if again := h.request(http.MethodGet, "/", nil, false); strings.Contains(again.Body.String(), "Retained refresh notification") {
			t.Fatal("displayed notification remained queued")
		}
	})
}

func TestHTTPNotificationRemovalStorageFailure(t *testing.T) {
	for _, operation := range []string{"UPDATE", "DELETE"} {
		t.Run(operation, func(t *testing.T) {
			runSiteScenario(t, func(h *httpHarness) {
				h.app.Web.HandleFunc("GET /test/seed-notification", func(w http.ResponseWriter, r *http.Request) {
					if err := flash.AddSuccessNotification(w, r, "Retained notification"); err != nil {
						t.Fatal(err)
					}
					if operation == "UPDATE" {
						if err := flash.AddMessage(w, r, "Other category", flash.AuthMessages); err != nil {
							t.Fatal(err)
						}
					}
				})
				h.request(http.MethodGet, "/test/seed-notification", nil, false)
				if _, err := h.db.Exec("CREATE TRIGGER reject_message_removal BEFORE " + operation + " ON session BEGIN SELECT RAISE(ABORT, 'private message storage failure'); END"); err != nil {
					t.Fatal(err)
				}
				for _, htmx := range []bool{false, true} {
					w := h.request(http.MethodGet, "/", nil, htmx)
					if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "Retained notification") || strings.Contains(w.Body.String(), "private message storage failure") {
						t.Fatalf("storage failure: status=%d, body=%q", w.Code, w.Body.String())
					}
					if len(w.Result().Cookies()) != 0 {
						t.Fatal("failed removal changed cookies")
					}
				}
				if !strings.Contains(h.logs.String(), "private message storage failure") {
					t.Fatal("storage failure was not logged")
				}
				if _, err := h.db.Exec("DROP TRIGGER reject_message_removal"); err != nil {
					t.Fatal(err)
				}
				w := h.request(http.MethodGet, "/", nil, false)
				if w.Code != http.StatusOK || strings.Count(w.Body.String(), "Retained notification") != 1 {
					t.Fatalf("retry did not display notification once: status=%d, body=%q", w.Code, w.Body.String())
				}
				w = h.request(http.MethodGet, "/", nil, false)
				if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Retained notification") {
					t.Fatal("notification replayed after successful removal")
				}
			}, func(cfg *config.Config) { cfg.HTTP.Session.Types["hs-message-session"] = "sqliteStore" })
		})
	}
}

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
	catalog := func(h *httpHarness) ([]handlers.Descriptor, error) {
		cm := h.app.ContentManager
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
		return siteCatalog(h, "")
	}
	runSiteScenarioWithCatalog(t, catalog, func(h *httpHarness) {
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
