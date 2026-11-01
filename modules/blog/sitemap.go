package blog

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/pkg/util"
)

// Single-document limits come from the sitemap protocol. Larger sites need an index.
const (
	maxSitemapURLs  = 50_000
	maxSitemapBytes = 50 * 1024 * 1024
)

// sitemapDocument lists preferred public URLs without optional ranking or date hints.
type sitemapDocument struct {
	XMLName xml.Name          `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []sitemapLocation `xml:"url"`
}

// sitemapLocation identifies one public page by its absolute canonical URL.
type sitemapLocation struct {
	URL string `xml:"loc"`
}

// robots allows crawling and advertises the sitemap using the configured origin.
// Access controls, not crawler instructions, protect private content.
func (m *module) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	sitemap, err := util.BuildPublicURL(r, m.app.Config.HTTP, "/sitemap.xml", nil)
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	text := "User-agent: *\nDisallow:\n\nSitemap: " + sitemap.String() + "\n"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "robots.txt", time.Time{}, strings.NewReader(text))
}

// sitemap serves current public metadata, regardless of the request's account.
// It rebuilds from lightweight repository lists so publication changes need no cache invalidation.
func (m *module) sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	origin, err := util.BuildPublicURL(r, m.app.Config.HTTP, "/", nil)
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	data, err := m.buildSitemap(r.Context(), origin)
	if err != nil {
		m.storageError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "sitemap.xml", time.Time{}, bytes.NewReader(data))
}

// buildSitemap excludes drafts and pages beneath unpublished ancestors. It builds
// the whole document before responding so failures cannot return a partial sitemap.
func (m *module) buildSitemap(ctx context.Context, origin *url.URL) ([]byte, error) {
	posts, err := m.posts.ListArchive(ctx)
	if err != nil {
		return nil, err
	}
	pages, err := m.pageStore.ListPages(ctx, false)
	if err != nil {
		return nil, err
	}
	count := 2 + len(posts) + len(pages)
	if count > maxSitemapURLs {
		return nil, errors.New("sitemap exceeds 50,000 URLs: multiple sitemaps are required")
	}
	document := sitemapDocument{URLs: make([]sitemapLocation, 0, count)}
	add := func(path string) {
		link := *origin
		link.Path = path
		document.URLs = append(document.URLs, sitemapLocation{URL: link.String()})
	}
	add("/")
	add("/posts")
	for _, post := range posts {
		add("/post/s/" + post.Slug)
	}
	for _, page := range pages {
		add(page.URL())
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := xml.Marshal(document)
	if err != nil {
		return nil, err
	}
	if len(xml.Header)+len(data) > maxSitemapBytes {
		return nil, errors.New("sitemap exceeds 50 MiB: multiple sitemaps are required")
	}
	return append([]byte(xml.Header), data...), nil
}
