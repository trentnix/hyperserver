package blog

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"net/url"
	"time"

	"github.com/trentnix/hyperserver/pkg/util"
)

// feed is the RSS 2.0 document envelope.
type feed struct {
	XMLName xml.Name    `xml:"rss"`
	Version string      `xml:"version,attr"`
	Channel feedChannel `xml:"channel"`
}

// feedChannel describes the blog and its published entries.
type feedChannel struct {
	Title       string     `xml:"title"`
	Link        string     `xml:"link"`
	Description string     `xml:"description"`
	Items       []feedItem `xml:"item"`
}

// feedItem carries a stable ID link and rendered post content for feed readers.
type feedItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	Published   string `xml:"pubDate"`
	Content     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
}

// rss serves cached XML with standard conditional and HEAD response handling.
// Public URLs come from configuration, never Host or forwarded headers.
func (m *module) rss(w http.ResponseWriter, r *http.Request) {
	origin, err := util.BuildPublicURL(r, m.app.Config.HTTP, "/", nil)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		m.storageError(w, r, err)
		return
	}
	document, err := m.feed.loadUntil(r.Context(), origin.String(), func() ([]byte, time.Time, error) {
		next, err := m.posts.NextPublication(r.Context())
		if err != nil {
			return nil, time.Time{}, err
		}
		data, err := m.buildFeed(r.Context(), origin)
		return data, next, err
	})
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		m.storageError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", document.etag)
	http.ServeContent(w, r, "feed.xml", time.Time{}, bytes.NewReader(document.data))
}

// buildFeed includes every published post. No request-specific account data enters the cache.
func (m *module) buildFeed(ctx context.Context, origin *url.URL) ([]byte, error) {
	posts, err := m.posts.List(ctx)
	if err != nil {
		return nil, err
	}
	profile := m.profile.Current()
	channel := feedChannel{Title: profile.Name, Link: origin.String(), Description: profile.Bio}
	for _, p := range posts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		link := *origin
		link.Path = "/post/s/" + p.Slug
		guid := *origin
		guid.Path = "/post/id/" + p.ID
		body, err := markdownAt(p.Markdown, &link)
		if err != nil {
			return nil, err
		}
		channel.Items = append(channel.Items, feedItem{p.Title, link.String(), guid.String(), p.Summary, p.Published.Format(time.RFC1123Z), string(body)})
	}
	data, err := xml.Marshal(feed{Version: "2.0", Channel: channel})
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}
