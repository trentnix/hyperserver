package blog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func registeredBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	return h
}

func setupBrowserHarness(t *testing.T) *harness {
	t.Helper()
	return startHarness(t, harnessConfig(t, 10))
}

func accountBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h, _ := accountHarness(t, true)
	return h
}

func authChoicesBrowserHarness(email, setupComplete bool) func(*testing.T) *harness {
	return func(t *testing.T) *harness {
		t.Helper()
		cfg := harnessConfig(t, 10)
		cfg.Auth.Services["google"] = map[string]string{"enabled": "true", "clientID": "test-client", "clientSecret": "test-secret"}
		if !email {
			cfg.Auth.Services["email"]["enabled"] = "false"
		}
		h := startHarness(t, cfg)
		if setupComplete {
			if err := h.blog.access.setup.finish(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		return h
	}
}

func tagsBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	if _, err := h.blog.persistPost(t.Context(), Post{Title: "Tag source", Markdown: "Body", Tags: "Go, self-hosting", Draft: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.blog.persistPage(t.Context(), Page{Title: "Other tags", Tags: "Go, SQLite", ChildrenDisplay: "hidden"}); err != nil {
		t.Fatal(err)
	}
	return h
}

func editorHistoryBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	if _, err := savePostFixture(h.blog.posts, t.Context(), Post{Slug: "cancel-source", Title: "Cancel source", Markdown: "Body", Published: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	h.blog.invalidatePosts()
	return h
}

func posterUploadBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	account, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	account.Role = string(rolePoster)
	if err := h.app.Accounts.Update(t.Context(), account); err != nil {
		t.Fatal(err)
	}
	return h
}

func profileHistoryBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	image := mediaUpload(t, h.blog, "historical profile image")
	for i := range 12 {
		p := h.blog.profile.Current()
		p.Name, p.Bio = fmt.Sprintf("Profile %02d", i), "A saved biography."
		p.ImageURL, p.FaviconURL, p.BackgroundURL = "", "", ""
		if i == 0 {
			p.ImageURL, p.FaviconURL, p.BackgroundURL = "/images/"+image.ID, "/images/"+image.ID, "/images/"+image.ID
		}
		if err := h.blog.persistProfile(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.blog.removeMedia(t.Context(), image.ID, image.Version(), true); err != nil {
		t.Fatal(err)
	}
	return h
}

func searchBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	seedPublicSearch(t, h)
	h.blog.recentPosts.invalidate()
	return h
}

func archiveBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	seedArchive(t, h, 2*archivePageSize+1)
	return h
}

func mediaLibraryBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	for i := range mediaPageSize + 1 {
		mediaUpload(t, h.blog, fmt.Sprintf("library image %d", i))
	}
	return h
}

func albumBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	for index := range maxAlbumPhotos {
		mediaUpload(t, h.blog, fmt.Sprintf("album photo %d", index))
	}
	return h
}

func formErrorsBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	mediaUpload(t, h.blog, "form errors image")
	return h
}

func mediaCleanupBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	mediaUpload(t, h.blog, "unused browser image")
	historical := mediaUpload(t, h.blog, "historical browser image")
	post, err := savePostFixture(h.blog.posts, t.Context(), Post{Slug: "historical-image", Title: "Historical image", Markdown: "![Old](/images/" + historical.ID + ")", PreviewImage: historical.ID, Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	post.Markdown, post.PreviewImage = "No images", ""
	if _, err := savePostFixture(h.blog.posts, t.Context(), post); err != nil {
		t.Fatal(err)
	}
	used := mediaUpload(t, h.blog, "saved browser image")
	if _, err := savePostFixture(h.blog.posts, t.Context(), Post{Slug: "image", Title: "Saved image", PreviewImage: used.ID, Draft: true}); err != nil {
		t.Fatal(err)
	}
	return h
}

func adminNavigationBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	seedContentSearch(t, h)
	if _, err := savePageFixture(h.blog.pageStore, t.Context(), Page{Slug: "visible-child", Title: "Visible child", ParentID: h.projectsID, Published: true}); err != nil {
		t.Fatal(err)
	}
	return h
}

func postManagementBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	for _, draft := range []bool{true, false} {
		for i, title := range []string{"Short title", strings.Repeat("A longer post title with several words ", 6), strings.Repeat("W", 240)} {
			if _, err := savePostFixture(h.blog.posts, t.Context(), Post{Slug: fmt.Sprintf("layout-%t-%d", draft, i), Title: title, Markdown: "Body", Draft: draft}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return h
}

func pagesBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	child, err := savePageFixture(h.blog.pageStore, t.Context(), Page{ParentID: h.projectsID, Slug: "sample-project", Title: "A sample project with a longer title", Published: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := savePageFixture(h.blog.pageStore, t.Context(), Page{ParentID: child.ID, Slug: "sample-guide", Title: "A draft guide"}); err != nil {
		t.Fatal(err)
	}
	return h
}

func navigationBrowserHarness(t *testing.T) *harness {
	t.Helper()
	h := registeredBrowserHarness(t)
	h.login(t)
	if _, err := savePageFixture(h.blog.pageStore, t.Context(), Page{Slug: "another-page", Title: "Another page with a longer title", Published: true}); err != nil {
		t.Fatal(err)
	}
	for i := range 6 {
		title := "Sample project"
		if i == 0 {
			title = strings.Repeat("A long project title ", 8) + `"notes" <guide>`
		} else if i == 1 {
			title = "Short"
		}
		_, err := savePageFixture(h.blog.pageStore, t.Context(), Page{ParentID: h.projectsID,
			Slug: fmt.Sprintf("project-%d", i), Title: title, Summary: "A project card for navigation layout checks.", Published: true,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	project, err := h.blog.pageStore.GetPageBySlug(t.Context(), "project-0", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := savePageFixture(h.blog.pageStore, t.Context(), Page{ParentID: project.ID, Slug: "project-guide", Title: "Project guide", Published: true}); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"first", "second"} {
		body := "Body of " + slug
		if slug == "first" {
			body = strings.Repeat(body+"\n\n", 100)
		}
		response := h.request(http.MethodPost, "/post", url.Values{
			"title": {slug + " <post>"}, "slug": {slug}, "content": {body},
		}, false)
		requireStatus(t, response, http.StatusSeeOther)
		h.publish(t, slug)
	}
	return h
}

func revisionBrowserScript(versions int) func(*testing.T, *harness, []byte) []byte {
	return func(t *testing.T, h *harness, script []byte) []byte {
		t.Helper()
		var targets []map[string]any
		store := h.blog.posts.(*sqliteRepository)
		for _, kind := range []string{"post", "page"} {
			original := Revision{Title: "Original title", Summary: "Original summary", Markdown: "# Original body"}
			id, version, err := saveRevisionContent(t.Context(), store, kind, "", 0, original)
			if err != nil {
				t.Fatal(err)
			}
			list, err := store.ListRevisions(t.Context(), kind, id)
			if err != nil || len(list) != 1 {
				t.Fatal(list, err)
			}
			_, version, err = saveRevisionContent(t.Context(), store, kind, id, version, Revision{Title: "Current title", Summary: "Current summary\r\nSecond line", Markdown: "Current body\r\nSecond line"})
			if err != nil {
				t.Fatal(err)
			}
			for i := 3; i <= versions; i++ {
				_, version, err = saveRevisionContent(t.Context(), store, kind, id, version, Revision{Title: fmt.Sprintf("Version %d", i), Markdown: "Saved writing"})
				if err != nil {
					t.Fatal(err)
				}
			}
			prefix := "/post"
			if kind == "page" {
				prefix = "/pages"
			}
			view, manager := "/post/s/history", "/post/manage"
			if kind == "page" {
				view, manager = "/page/history", "/pages"
			}
			targets = append(targets, map[string]any{"editor": prefix + "/update/" + id, "history": "/history/" + kind + "/" + id, "revision": list[0].ID, "view": view, "manager": manager})
		}
		data, err := json.Marshal(targets)
		if err != nil {
			t.Fatal(err)
		}
		script = bytes.ReplaceAll(script, []byte("__REVISION_TARGETS__"), data)
		return script
	}
}

func resetBrowserScript(t *testing.T, h *harness, script []byte) []byte {
	t.Helper()
	requireStatus(t, h.request("POST", "/auth/reset/request/email", url.Values{"email": {"trentnix@gmail.com"}}, false), http.StatusOK)
	if len(h.mail.messages) != 2 {
		t.Fatal("expected one reset email after registration")
	}
	link := regexp.MustCompile(`https://blog.example/auth/reset/email\?[^"< ]+`).FindString(h.mail.messages[1].Body)
	resetURL, err := url.Parse(html.UnescapeString(link))
	if err != nil || resetURL.Query().Get("token") == "" {
		t.Fatal("missing reset link", err)
	}
	script = bytes.ReplaceAll(script, []byte("__RESET_URL__"), []byte(fmt.Sprintf("%q", resetURL.RequestURI())))
	return script
}

func archiveBrowserScript(_ *testing.T, _ *harness, script []byte) []byte {
	return bytes.ReplaceAll(script, []byte("__ARCHIVE_PAGE_SIZE__"), []byte(fmt.Sprint(archivePageSize)))
}

func accountBrowserRoutes(h *harness) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/test/password-setup-link": func(w http.ResponseWriter, r *http.Request) {
			h.mail.mu.Lock()
			defer h.mail.mu.Unlock()
			if len(h.mail.messages) != 1 {
				http.Error(w, "setup mail missing", http.StatusNotFound)
				return
			}
			match := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(h.mail.messages[0].Body)
			if len(match) != 2 {
				http.Error(w, "setup link missing", http.StatusNotFound)
				return
			}
			link, err := url.Parse(html.UnescapeString(match[1]))
			if err != nil {
				http.Error(w, "invalid setup link", http.StatusInternalServerError)
				return
			}
			io.WriteString(w, link.RequestURI())
		},
	}
}

func setupBrowserRoutes(h *harness) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/test/verification-link": func(w http.ResponseWriter, r *http.Request) {
			h.mail.mu.Lock()
			defer h.mail.mu.Unlock()
			if len(h.mail.messages) != 1 {
				http.Error(w, "verification mail missing", http.StatusNotFound)
				return
			}
			link := regexp.MustCompile(`https://blog.example/auth/verify\?[^"< ]+`).FindString(h.mail.messages[0].Body)
			u, err := url.Parse(html.UnescapeString(link))
			if err != nil || u.Query().Get("token") == "" {
				http.Error(w, "verification token missing", http.StatusNotFound)
				return
			}
			io.WriteString(w, u.RequestURI())
		},
	}
}
