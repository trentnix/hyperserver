package blog

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestHomeNavigation keeps Home first in each menu, including HTMX updates,
// without adding another Home link inside nested page lists.
func TestHomeNavigation(t *testing.T) {
	const home = `<li><a href="/">Home</a></li>`
	homeBeforePage := regexp.MustCompile(regexp.QuoteMeta(home) + `\s*<li>\s*<a href="/page/parent"`)
	templates, err := template.ParseFS(blogModuleFS, "templates/profile.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, pages := range [][]pageLink{
		nil,
		{{Page: Page{ID: "parent", Slug: "parent", Title: "Parent"}, Children: []pageLink{
			{Page: Page{ParentID: "parent", Slug: "child", Title: "Child"}},
		}}},
	} {
		for _, tc := range []struct {
			name  string
			count int
		}{
			{"blog.profile", 1},
			{"blog.siteLinks", 1},
			{"blog.pageLinkUpdates", 2},
		} {
			var body strings.Builder
			data := struct{ LayoutData navigation }{navigation{Pages: pages}}
			if err := templates.ExecuteTemplate(&body, tc.name, data); err != nil {
				t.Fatal(err)
			}
			html := body.String()
			if strings.Count(html, home) != tc.count {
				t.Fatalf("%s: expected %d Home links: %s", tc.name, tc.count, html)
			}
			if len(pages) != 0 && len(homeBeforePage.FindAllStringIndex(html, -1)) != tc.count {
				t.Fatalf("%s: Home must precede page links: %s", tc.name, html)
			}
		}
	}
}

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
			r.Header.Set("HX-Target", "div#article-content")
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

func TestAuthenticationPagesOmitReaderSearch(t *testing.T) {
	cfg := harnessConfig(t, 10)
	cfg.Auth.Services["google"] = map[string]string{"enabled": "true", "clientID": "test-client", "clientSecret": "test-secret"}
	h := startHarness(t, cfg)
	// Exercise the normal site shell, not the separate onboarding layout.
	if err := h.blog.access.setup.finish(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/login", "/register", "/register?provider=email", "/auth/reset/request/email", "/"} {
		for _, mode := range []string{"page", "fragment", "history"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				r := httptest.NewRequest(http.MethodGet, path, nil)
				if mode != "page" {
					r.Header.Set("HX-Request", "true")
					r.Header.Set("HX-Target", "div#article-content")
				}
				if mode == "history" {
					r.Header.Set("HX-History-Restore-Request", "true")
				}
				w := httptest.NewRecorder()
				h.app.Handler.ServeHTTP(w, r)
				requireStatus(t, w, http.StatusOK)
				body := w.Body.String()
				if strings.Contains(body, "reader-search-form") != (path == "/") {
					t.Fatal("incorrect reader search visibility", body)
				}
				if mode != "page" && !strings.Contains(body, `hx-swap-oob="innerHTML:[data-reader-search-slot]"`) {
					t.Fatal("navigation must update existing search slots")
				}
			})
		}
	}
}
