package blog

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// sitemapURLs decodes the public format independently of the implementation types.
func sitemapURLs(t *testing.T, data []byte) []string {
	t.Helper()
	var document struct {
		XMLName xml.Name
		URLs    []string `xml:"url>loc"`
	}
	if err := xml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.XMLName.Local != "urlset" || document.XMLName.Space != "http://www.sitemaps.org/schemas/sitemap/0.9" {
		t.Fatal("incorrect sitemap namespace", document.XMLName)
	}
	slices.Sort(document.URLs)
	if len(slices.Compact(slices.Clone(document.URLs))) != len(document.URLs) {
		t.Fatal("duplicate sitemap URLs")
	}
	return document.URLs
}

// TestSitemapPublication follows current post and page visibility, even for admins.
func TestSitemapPublication(t *testing.T) {
	h := newHarness(t)
	savePage := func(p Page) Page {
		t.Helper()
		p, err := h.blog.pageStore.SavePage(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	post, err := h.blog.posts.Save(t.Context(), Post{Slug: "public-post", Title: "Public post"})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := h.blog.posts.Save(t.Context(), Post{Slug: "draft-post", Title: "Draft post", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	parent := savePage(Page{Slug: "parent", Title: "Parent"})
	child := savePage(Page{Slug: "child", Title: "Child", ParentID: parent.ID, Published: true})
	savePage(Page{Slug: "grandchild", Title: "Grandchild", ParentID: child.ID, Published: true})
	savePage(Page{Slug: "draft-child", Title: "Draft child", ParentID: parent.ID})

	check := func(paths ...string) []byte {
		t.Helper()
		response := h.request("GET", "/sitemap.xml", nil, false)
		requireStatus(t, response, http.StatusOK)
		want := []string{"https://blog.example/", "https://blog.example/posts", "https://blog.example/about", "https://blog.example/projects"}
		for _, path := range paths {
			want = append(want, "https://blog.example"+path)
		}
		slices.Sort(want)
		if got := sitemapURLs(t, response.Body.Bytes()); !slices.Equal(got, want) {
			t.Fatalf("URLs=%v, want %v", got, want)
		}
		for _, address := range want {
			page := h.request("GET", strings.TrimPrefix(address, "https://blog.example"), nil, false)
			requireStatus(t, page, http.StatusOK)
			requireCanonical(t, page.Body.String(), address)
		}
		return response.Body.Bytes()
	}
	anonymous := check("/post/s/public-post")
	h.register(t)
	h.verify(t)
	h.login(t)
	if admin := check("/post/s/public-post"); !bytes.Equal(admin, anonymous) {
		t.Fatal("administrator session changed the sitemap")
	}
	parent.Published = true
	parent = savePage(parent)
	check("/post/s/public-post", "/page/parent", "/page/child", "/page/grandchild")
	parent.Published = false
	savePage(parent)
	check("/post/s/public-post")

	if _, err := h.blog.posts.SetPublished(t.Context(), post.ID, false); err != nil {
		t.Fatal(err)
	}
	check()
	if draft, err = h.blog.posts.SetPublished(t.Context(), draft.ID, true); err != nil {
		t.Fatal(err)
	}
	check("/post/s/draft-post")
	draft.Slug, draft.Draft = "renamed", false
	if _, err := h.blog.posts.Save(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	check("/post/s/renamed")
	if err := h.blog.posts.Delete(t.Context(), draft.ID); err != nil {
		t.Fatal(err)
	}
	check()
	child.ParentID, child.Slug = "", "moved"
	savePage(child)
	check("/page/moved", "/page/grandchild")
}

// TestSitemapHTTP checks trusted absolute URLs, HEAD, fragments, and an empty site.
func TestSitemapHTTP(t *testing.T) {
	h := newHarness(t)
	pages, err := h.blog.pageStore.ListPages(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range pages {
		if err := h.blog.pageStore.DeletePage(t.Context(), page.ID); err != nil {
			t.Fatal(err)
		}
	}
	h.blog.app.Config.HTTP.PublicOrigin = "https://blog.example:8443"
	var size string
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, "http://spoofed.example/sitemap.xml?tracking=1", nil)
		r.Header.Set("Forwarded", "host=forwarded.example;proto=http")
		r.Header.Set("X-Forwarded-Host", "forwarded.example")
		r.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		h.app.Handler.ServeHTTP(w, r)
		requireStatus(t, w, http.StatusOK)
		if w.Header().Get("Content-Type") != "application/xml; charset=utf-8" || w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal("incorrect sitemap headers", w.Header())
		}
		if method == "HEAD" {
			if w.Body.Len() != 0 || w.Header().Get("Content-Length") != size {
				t.Fatal("incorrect HEAD response", w.Header(), w.Body.Len())
			}
			continue
		}
		size = strconv.Itoa(w.Body.Len())
		if got := sitemapURLs(t, w.Body.Bytes()); !slices.Equal(got, []string{"https://blog.example:8443/", "https://blog.example:8443/posts"}) {
			t.Fatal("untrusted origin or incorrect empty-site URLs", got)
		}
	}
	// The application's fallback handles methods without a registered route.
	requireStatus(t, h.request("POST", "/sitemap.xml", nil, false), http.StatusNotFound)
}

// TestRobotsHTTP checks unrestricted crawling, trusted sitemap URLs, and HEAD.
// robots.txt must not need content storage or change its output for HTMX requests.
func TestRobotsHTTP(t *testing.T) {
	h := newHarness(t)
	h.blog.posts = sitemapPosts{err: errors.New("posts unavailable")}
	h.blog.pageStore = sitemapPages{err: errors.New("pages unavailable")}
	for _, tc := range []struct {
		origin, host, want string
		port               uint16
	}{
		{"https://blog.example", "127.0.0.1", "https://blog.example/sitemap.xml", 8086},
		{"https://blog.example:8443", "127.0.0.1", "https://blog.example:8443/sitemap.xml", 8086},
		{"", "127.0.0.1", "http://127.0.0.1:8086/sitemap.xml", 8086},
		{"", "::1", "http://[::1]/sitemap.xml", 80},
	} {
		h.blog.app.Config.HTTP.PublicOrigin = tc.origin
		h.blog.app.Config.HTTP.ListenHost = tc.host
		h.blog.app.Config.HTTP.Port = tc.port
		want := "User-agent: *\nDisallow:\n\nSitemap: " + tc.want + "\n"
		for _, method := range []string{"GET", "HEAD"} {
			r := httptest.NewRequest(method, "http://spoofed.example/robots.txt?tracking=1", nil)
			r.Header.Set("Forwarded", "host=forwarded.example;proto=https")
			r.Header.Set("X-Forwarded-Host", "forwarded.example")
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("HX-Request", "true")
			w := httptest.NewRecorder()
			h.app.Handler.ServeHTTP(w, r)
			requireStatus(t, w, http.StatusOK)
			if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("Content-Length") != strconv.Itoa(len(want)) {
				t.Fatal("incorrect robots headers", w.Header())
			}
			if (method == "GET" && w.Body.String() != want) || (method == "HEAD" && w.Body.Len() != 0) {
				t.Fatalf("%s robots body=%q, want %q (empty for HEAD)", method, w.Body.String(), want)
			}
		}
	}
}

// TestRobotsInvalidOrigin refuses to advertise a URL when configuration cannot supply one.
func TestRobotsInvalidOrigin(t *testing.T) {
	h := newHarness(t)
	for _, origin := range []string{"invalid origin", "https://blog.example\nDisallow: /", ""} {
		h.blog.app.Config.HTTP.PublicOrigin = origin
		h.blog.app.Config.HTTP.ListenHost = "0.0.0.0"
		w := h.request("GET", "/robots.txt", nil, false)
		requireStatus(t, w, http.StatusInternalServerError)
		if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "Sitemap:") || strings.Contains(w.Body.String(), "Disallow:") {
			t.Fatal("invalid origin produced cached or partial crawler instructions")
		}
	}
}

// sitemapPosts supplies metadata or a controlled failure without loading post bodies.
type sitemapPosts struct {
	repository
	posts []ArchiveEntry
	err   error
}

func (s sitemapPosts) ListArchive(ctx context.Context) ([]ArchiveEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.posts, s.err
}

// sitemapPages enforces the public-only query and supplies controlled page results.
type sitemapPages struct {
	pageRepository
	pages []Page
	err   error
}

func (s sitemapPages) ListPages(ctx context.Context, includeDrafts bool) ([]Page, error) {
	if includeDrafts {
		return nil, errors.New("sitemap requested private pages")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.pages, s.err
}

// TestSitemapFailures rejects incomplete results, invalid origins, and cancelled work.
func TestSitemapFailures(t *testing.T) {
	for _, failure := range []string{"posts", "pages", "origin"} {
		t.Run(failure, func(t *testing.T) {
			h := newHarness(t)
			privateError := errors.New("private repository details")
			switch failure {
			case "posts":
				h.blog.posts = sitemapPosts{err: privateError}
			case "pages":
				h.blog.pageStore = sitemapPages{err: privateError}
			case "origin":
				h.blog.app.Config.HTTP.PublicOrigin = "invalid origin"
			}
			w := h.request("GET", "/sitemap.xml", nil, false)
			requireStatus(t, w, http.StatusInternalServerError)
			if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "<urlset") || strings.Contains(w.Body.String(), privateError.Error()) {
				t.Fatal("failure cached or exposed a partial sitemap or private details")
			}
		})
	}
	h := newHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	origin, _ := url.Parse("https://blog.example/")
	if data, err := h.blog.buildSitemap(ctx, origin); !errors.Is(err, context.Canceled) || data != nil {
		t.Fatal("cancelled build returned data", err)
	}
}

// TestSitemapEncoding keeps URL escaping separate from XML escaping.
func TestSitemapEncoding(t *testing.T) {
	origin, _ := url.Parse("https://blog.example/")
	m := &module{
		posts:     sitemapPosts{posts: []ArchiveEntry{{Slug: "café-&-tea", Title: "Not part of the sitemap"}}},
		pageStore: sitemapPages{},
	}
	data, err := m.buildSitemap(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://blog.example/post/s/caf%C3%A9-&-tea"
	if !slices.Contains(sitemapURLs(t, data), want) || !bytes.Contains(data, []byte("&amp;")) || bytes.Contains(data, []byte("Not part of the sitemap")) {
		t.Fatal("incorrect URL or XML encoding", string(data))
	}
}

// TestSitemapLimits rejects oversized output instead of silently omitting content.
func TestSitemapLimits(t *testing.T) {
	origin, _ := url.Parse("https://blog.example/")
	posts := make([]ArchiveEntry, maxSitemapURLs-2)
	for i := range posts {
		posts[i].Slug = fmt.Sprintf("post-%d", i)
	}
	m := &module{posts: sitemapPosts{posts: posts}, pageStore: sitemapPages{}}
	data, err := m.buildSitemap(t.Context(), origin)
	if err != nil || len(sitemapURLs(t, data)) != maxSitemapURLs {
		t.Fatal("valid URL limit rejected", err)
	}
	m.pageStore = sitemapPages{pages: []Page{{Slug: "extra", Published: true}}}
	if data, err := m.buildSitemap(t.Context(), origin); err == nil || data != nil {
		t.Fatal("excessive URL count accepted", err)
	}
	m.pageStore = sitemapPages{}
	// Long origins can exceed the byte limit before the URL-count limit.
	origin.Host = strings.Repeat("a", 1100) + ".example"
	if data, err := m.buildSitemap(t.Context(), origin); err == nil || data != nil {
		t.Fatal("oversized sitemap accepted", err)
	}
}
