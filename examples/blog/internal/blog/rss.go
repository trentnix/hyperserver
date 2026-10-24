package blog

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/pkg/util"
	"golang.org/x/net/html"
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
	document, err := m.feed.load(r.Context(), origin.String(), func() ([]byte, error) {
		return m.buildFeed(r.Context(), origin)
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
	channel := feedChannel{Title: m.app.Config.App.Name, Link: origin.String(), Description: "Building and delivering better software."}
	for _, p := range posts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		link := *origin
		link.Path = "/post/s/" + p.Slug
		guid := *origin
		guid.Path = "/post/id/" + p.ID
		body, err := feedHTML(p.Markdown, &link)
		if err != nil {
			return nil, err
		}
		channel.Items = append(channel.Items, feedItem{p.Title, link.String(), guid.String(), p.Summary, p.Published.Format(time.RFC1123Z), body})
	}
	data, err := xml.Marshal(feed{Version: "2.0", Channel: channel})
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}

// feedHTML resolves rendered link and image URLs against the post's public URL.
// The shared Markdown renderer still removes raw HTML and unsafe URL schemes.
func feedHTML(source string, base *url.URL) (string, error) {
	rendered, err := markdown(source)
	if err != nil {
		return "", err
	}
	tokens := html.NewTokenizer(strings.NewReader(string(rendered)))
	var out strings.Builder
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			if tokens.Err() == io.EOF {
				return out.String(), nil
			}
			return "", tokens.Err()
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			for i, attr := range token.Attr {
				if attr.Val == "" || !(token.Data == "a" && attr.Key == "href" || token.Data == "img" && attr.Key == "src") {
					continue
				}
				ref, err := url.Parse(attr.Val)
				if err == nil && !ref.IsAbs() {
					token.Attr[i].Val = base.ResolveReference(ref).String()
				}
			}
			out.WriteString(token.String())
		default:
			out.Write(tokens.Raw())
		}
	}
}
