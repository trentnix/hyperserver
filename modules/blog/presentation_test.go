package blog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// navigationReads counts shell-only queries without changing page storage behavior.
type navigationReads struct {
	pageRepository
	calls int
}

func (s *navigationReads) ListPages(ctx context.Context, drafts bool) ([]Page, error) {
	s.calls++
	return s.pageRepository.ListPages(ctx, drafts)
}

// TestNavigationOnlyForShell keeps fragment rendering independent of shell data.
func TestNavigationOnlyForShell(t *testing.T) {
	h := newHarness(t)
	store := &navigationReads{pageRepository: h.blog.pageStore}
	h.blog.pageStore = store
	for _, tc := range []struct {
		name        string
		hx, restore bool
		setup       bool
		reads       int
	}{
		{"ordinary", false, false, false, 1},
		{"fragment", true, false, false, 0},
		{"history restore", true, true, false, 1},
		{"setup", false, false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store.calls = 0
			r := httptest.NewRequest(http.MethodGet, "https://blog.example/", nil)
			if tc.setup {
				r = r.WithContext(context.WithValue(r.Context(), setupPageKey{}, true))
			}
			if tc.hx {
				r.Header.Set("HX-Request", "true")
			}
			if tc.restore {
				r.Header.Set("HX-History-Restore-Request", "true")
			}
			w := httptest.NewRecorder()
			h.app.Handler.ServeHTTP(w, r)
			requireStatus(t, w, http.StatusOK)
			if store.calls != tc.reads {
				t.Fatalf("navigation queries = %d, want %d", store.calls, tc.reads)
			}
			if tc.reads != 0 && !strings.Contains(w.Body.String(), `href="/about"`) {
				t.Fatal("full shell lost page navigation")
			}
		})
	}
}

// TestAdministrationScripts limits editor and media behavior to admin shells.
func TestAdministrationScripts(t *testing.T) {
	h := newHarness(t)
	w := h.request(http.MethodGet, "/", nil, false)
	for _, script := range []string{"admin.js", "editor.js", "media.js", "editor-drafts.js", "easymde.min.js"} {
		if strings.Contains(w.Body.String(), script) {
			t.Fatalf("public shell loads %s", script)
		}
	}
	h.register(t)
	h.verify(t)
	h.login(t)
	w = h.request(http.MethodGet, "/", nil, false)
	if !strings.Contains(w.Body.String(), `type="module" src="/assets/js/admin.js"`) {
		t.Fatal("admin shell must load its native module entry point")
	}
}

// TestReaderSearchSwaps covers navigation and history without touching search slots on editor swaps.
func TestReaderSearchSwaps(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	for _, path := range []string{"/", "/pages"} {
		for _, restore := range []bool{false, true} {
			r := httptest.NewRequest("GET", path, nil)
			r.Header.Set("HX-Request", "true")
			r.Header.Set("HX-Target", "article-content")
			if restore {
				r.Header.Set("HX-History-Restore-Request", "true")
			}
			for _, cookie := range h.cookies {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			h.app.Handler.ServeHTTP(w, r)
			requireStatus(t, w, http.StatusOK)
			if !strings.Contains(w.Body.String(), `hx-swap-oob="innerHTML:[data-reader-search-slot]"`) {
				t.Fatal("article navigation must update the shell search slots", path, restore)
			}
			if path == "/pages" && strings.Contains(w.Body.String(), "reader-search-form") {
				t.Fatal("admin pages must clear reader search")
			}
		}
	}
	w := h.request("GET", "/media?pick=1", nil, true)
	if strings.Contains(w.Body.String(), `hx-swap-oob="innerHTML:[data-reader-search-slot]"`) {
		t.Fatal("media chooser must not change shell search")
	}
}
